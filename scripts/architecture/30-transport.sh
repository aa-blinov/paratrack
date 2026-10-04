# Sourced by scripts/check-architecture.sh; do not execute directly.
# HTTP 500 responses must use the shared error writer so the underlying
# failure is logged and the response does not expose implementation details.
for source in internal/web/*.go; do
	case "$source" in
		internal/web/responses.go|*_test.go) continue ;;
	esac
	if grep -Eq 'http\.Error\(w, .*[, ](500|http\.StatusInternalServerError)\)|writeJSONStatus\(w, (500|http\.StatusInternalServerError)|WriteHeader\((500|http\.StatusInternalServerError)\)' "$source"; then
		echo "architecture check: HTTP 500 responses must use the shared internal error writer ($source)" >&2
		exit 1
	fi
done

# Direct registrations on the outer mux bypass both protected route helpers.
# Keep them limited to the reviewed public surface and the protected page mount.
public_mux_routes=$(sed -nE 's/^[[:space:]]*mux\.Handle(Func)?\("([^"]+)".*/\2/p' internal/web/routes.go)
while IFS= read -r route; do
	case "$route" in
		"GET /login"|"GET /register"|"GET /lang/{code}"|\
		"POST /api/login"|"POST /api/register"|"POST /api/logout"|\
		"GET /static/"|"GET /sw.js"|"GET /static/manifest.webmanifest"|\
		"POST /api/stripe/webhook"|"POST /sentry-tunnel"|\
		"GET /forgot-password"|"GET /reset-password"|\
		"POST /api/password/forgot"|"POST /api/password/reset"|\
		"GET /sso/login"|"GET /sso/callback"|"/")
			;;
		*)
			echo "architecture check: outer HTTP mux route must be explicitly reviewed as public or the protected pages mount ($route)" >&2
			exit 1
			;;
	esac
done <<EOF
$public_mux_routes
EOF
route_sources=$(find internal/web -maxdepth 1 -type f -name 'routes*.go' -print)
if ! grep -Eq 'mux\.Handle\("/",[[:space:]]*pageAuth\(pages\)\)' $route_sources; then
	echo "architecture check: protected page routes must mount behind pageAuth" >&2
	exit 1
fi
if ! grep -Eq 'mux\.Handle\(method\+" "\+path,[[:space:]]*apiAuth\(h\)\)' $route_sources; then
	echo "architecture check: JSON API routes must mount behind apiAuth" >&2
	exit 1
fi
protected_api_routes=""
for route_source in $route_sources; do
	protected_api_routes="$protected_api_routes
$(sed -n '/^func (s \*Server) registerProtectedAPIRoutes(/,/^}/p' "$route_source")"
done
if ! printf '%b\n' "$protected_api_routes" | grep -q '^func (s \*Server) registerProtectedAPIRoutes('; then
	echo "architecture check: protected API route registration function is missing" >&2
	exit 1
fi
direct_api_registration=$(printf '%s\n' "$protected_api_routes" |
	grep -E 'mux\.Handle(Func)?\(' |
	grep -Ev 'mux\.Handle\(method\+" "\+path,[[:space:]]*apiAuth\(h\)\)' || true)
if [ -n "$direct_api_registration" ]; then
	echo "architecture check: protected API routes must register through the apiAuth helper" >&2
	printf '%s\n' "$direct_api_registration" >&2
	exit 1
fi

# Route handlers own transport responses. Shared behavior belongs in helpers
# or workflows, so one handler cannot accidentally return another route's
# HTML/HTMX response from a JSON or unrelated route.
handler_chaining=$(grep -REn '\.[[:space:]]*handle[A-Z][A-Za-z0-9]*\(w,[[:space:]]*r\)' internal/web --include='*.go' --exclude='*_test.go' || true)
if [ -n "$handler_chaining" ]; then
	echo "architecture check: HTTP handlers must share helpers, not call other handlers:" >&2
	printf '%s\n' "$handler_chaining" >&2
	exit 1
fi

# Endpoint handlers must reject malformed forms before invoking mutations.
# The CSRF middleware is the only exception: it probes the form for a token
# and rejects requests without a valid token before the endpoint runs.
ignored_form_parses=$(grep -RFn '_ = r.ParseForm()' internal/web --include='*.go' --exclude='*_test.go' --exclude='security.go' || true)
if [ -n "$ignored_form_parses" ]; then
	echo "architecture check: HTTP endpoint handlers must check ParseForm errors:" >&2
	printf '%s\n' "$ignored_form_parses" >&2
	exit 1
fi

# Direct audit persistence is kept behind the transport audit adapter. Route
# handlers may request an audit action, but cannot write through the port ad hoc.
audit_record_calls=$(grep -Rho 'AuditLog\.Record[[:space:]]*(' internal/web --include='*.go' --exclude='*_test.go' | wc -l | tr -d ' ')
if [ "$audit_record_calls" != "1" ] ||
	! grep -Eq 'AuditLog\.Record[[:space:]]*\(' internal/web/audit_adapter.go; then
	echo "architecture check: HTTP audit writes must go through internal/web/audit_adapter.go" >&2
	exit 1
fi

# Request handlers use the request-aware clock so date windows respect the
# caller's timezone and one request does not mix clock sources.
if grep -Eq 'time\.Now\(' internal/web/usertz.go; then
	echo "architecture check: HTTP userNow must use the request clock, not the process clock" >&2
	exit 1
fi
if ! grep -Eq 'return withRequestClock\(' internal/web/routes.go; then
	echo "architecture check: every HTTP route must be wrapped with the request clock" >&2
	exit 1
fi
for source in internal/web/handler*.go; do
	case "$source" in *_test.go) continue ;; esac
	if grep -Eq 'time\.Now\(' "$source"; then
		echo "architecture check: HTTP handlers must use userNow instead of the process clock ($source)" >&2
		exit 1
	fi
done

# The HTTP server and log-backed mail adapter consume process-owned runtime
# dependencies; they must not silently select package-global defaults.
if grep -Eq 'log\.Default\(|time\.Now\(' internal/web/server.go ||
	! grep -q 'config.Logger == nil' internal/web/server.go ||
	! grep -q 'config.Now == nil' internal/web/server.go ||
	! grep -q 'ErrMissingLogger' internal/web/server.go ||
	! grep -q 'ErrMissingClock' internal/web/server.go; then
	echo "architecture check: HTTP server must require the process logger and clock" >&2
	exit 1
fi
if grep -q 'log.Default()' internal/mail/mail.go ||
	! grep -q 'logger == nil' internal/mail/mail.go ||
	! grep -q 'return nil, ErrMissingLogger' internal/mail/mail.go; then
	echo "architecture check: log-backed mail sender must require an injected logger" >&2
	exit 1
fi
if grep -q 'context.Background()' internal/mail/mail.go ||
	! grep -q 'return ErrNilDeliveryContext' internal/mail/mail.go; then
	echo "architecture check: SMTP delivery must preserve caller cancellation and reject nil contexts" >&2
	exit 1
fi

# CLI commands share the runtime clock so time-sensitive behavior can be
# deterministic in callers and tests. Only the runtime resolves the default.
runtime_now=$(sed -n '/^func (rt \*Runtime) now(/,/^}/p' internal/cli/runtime.go)
if printf '%s\n' "$runtime_now" | grep -Eq 'time\.(Now|Since|Until)\('; then
	echo "architecture check: CLI runtime clock accessor must not silently resolve wall time" >&2
	exit 1
fi
for source in internal/cli/*.go; do
	case "$source" in internal/cli/runtime.go|*_test.go) continue ;; esac
	if grep -Eq 'time\.(Now|Since|Until)\(' "$source"; then
		echo "architecture check: CLI commands must use the runtime clock ($source)" >&2
		exit 1
	fi
done

# Shared domain records are not a transport schema. API encoders and JSON
# field names belong to transport DTOs; json:"-" is allowed only to ensure
# sensitive or internal state cannot be serialized by accident.
for source in internal/model/*.go; do
	case "$source" in
		*_test.go) continue ;;
	esac
	if grep -Eq '`json:"[^-]' "$source"; then
		echo "architecture check: JSON transport tags belong in adapter DTOs ($source)" >&2
		exit 1
	fi
done

# Keep JSON response headers and encoding behavior centralized in one adapter
# helper, including non-API JSON media types such as the PWA manifest.
for source in internal/web/*.go; do
	case "$source" in internal/web/responses.go|*_test.go) continue ;; esac
	if grep -Eq 'json\.NewEncoder\(' "$source"; then
		echo "architecture check: HTTP JSON output must use the shared response encoder ($source)" >&2
		exit 1
	fi
done

# API token page data combines auth tokens with team labels in the token
# management workflow; the transport must not assemble this cross-feature view.
token_page=$(sed -n '/^func (s \*Server) renderTokens(/,/^}/p' internal/web/handlers_tokens.go)
if printf '%s\n' "$token_page" | grep -q 'Teams\.Directory\.FindByIDs' ||
	! printf '%s\n' "$token_page" | grep -q 'TokenAdmin\.Management'; then
	echo "architecture check: API token page must use the token management read workflow" >&2
	exit 1
fi

# Focusing an activity by its UI name is one tracking use case, including
# lookup and the audited transition; the handler must not sequence two ports.
focus_handler=$(sed -n '/^func (s \*Server) handleFocus(/,/^}/p' internal/web/handlers_timers.go)
if printf '%s\n' "$focus_handler" | grep -q 'Tracking\.Queries\.FindActivity' ||
	! printf '%s\n' "$focus_handler" | grep -q 'TrackingOps\.FocusActivity'; then
	echo "architecture check: activity focus must use the coordinated tracking operation" >&2
	exit 1
fi

# Starting from an imported task coordinates the integration lookup and timer
# start in an application workflow, not in the HTTP adapter.
integration_start_handler=$(sed -n '/^func (s \*Server) handleIntegrationStart(/,/^}/p' internal/web/handlers_integrations.go)
if printf '%s\n' "$integration_start_handler" | grep -q 'Integrations\.Queries\.Task' ||
	! printf '%s\n' "$integration_start_handler" | grep -q 'ImportedTaskTracking\.Start'; then
	echo "architecture check: imported-task timer start must use its coordinating workflow" >&2
	exit 1
fi

# Workspace section settings must be read through the request-scoped cache so
# route guards and page navigation share one consistent snapshot.
module_settings_reads=$(grep -Rho 'Settings\.SectionModules[[:space:]]*(' internal/web --include='*.go' --exclude='*_test.go' | wc -l | tr -d ' ')
if [ "$module_settings_reads" != "1" ] ||
	! grep -q 'ctxTeamModulesKey, &teamModulesCache{}' internal/web/middleware.go ||
	! grep -q 'cache.load(read)' internal/web/modules.go; then
	echo "architecture check: workspace section reads must use the request-scoped module cache" >&2
	exit 1
fi

# Shared HTML layout data must use narrow view models instead of auth/domain
# records, which may contain fields that templates should never receive. Check
# the positive shape too, so renaming a domain type cannot bypass this rule.
page_data=$(sed -n '/^type pageData struct {/,/^}/p' internal/web/view_base.go)
if ! printf '%s\n' "$page_data" | grep -Eq '^[[:space:]]*User[[:space:]]+\*userView([[:space:]]|$)' ||
	! printf '%s\n' "$page_data" | grep -Eq '^[[:space:]]*Team[[:space:]]+\*teamView([[:space:]]|$)'; then
	echo "architecture check: pageData must expose userView and teamView" >&2
	exit 1
fi
# Presentation structs may contain explicitly adapted scalar fields, but must
# not expose shared domain records directly to templates or JSON encoders.
domain_record_fields=$(grep -REn '^[[:space:]]+[A-Z][A-Za-z0-9_]*[[:space:]]+(\[\])?model\.' internal/web --include='*.go' --exclude='*_test.go' || true)
if [ -n "$domain_record_fields" ]; then
	echo "architecture check: transport DTOs must adapt shared model records into presentation types:" >&2
	printf '%s\n' "$domain_record_fields" >&2
	exit 1
fi
user_view=$(sed -n '/^type userView struct {/,/^}/p' internal/web/view_base.go)
if printf '%s\n' "$user_view" | grep -Eiq 'password|hash|token|secret'; then
	echo "architecture check: HTML user view must not expose credentials or secrets" >&2
	exit 1
fi
if grep -Eq '^[[:space:]]*(Project|Projects)[[:space:]]+(\[\])?model\.Project([[:space:]]|$)' internal/web/*.go; then
	echo "architecture check: HTML page data must use projectView instead of model.Project" >&2
	exit 1
fi
if grep -Eq '^[[:space:]]*(Activities|Others)[[:space:]]+\[\]model\.Activity([[:space:]]|$)' internal/web/*.go; then
	echo "architecture check: HTML page data must use activityView instead of model.Activity" >&2
	exit 1
fi
if grep -Eq '^[[:space:]]*(Unassigned|SavedReports)[[:space:]]+\[\]model\.(UnassignedActivity|SavedReport)([[:space:]]|$)|^[[:space:]]*Billing[[:space:]]+model\.BillingRules([[:space:]]|$)' internal/web/*.go; then
	echo "architecture check: HTML page data must use presentation DTOs for billing and saved reports" >&2
	exit 1
fi
if grep -Eq '^[[:space:]]*Invites[[:space:]]+\[\]teams\.Invite([[:space:]]|$)|^[[:space:]]*Invite[[:space:]]+teams\.Invite([[:space:]]|$)' internal/web/*.go; then
	echo "architecture check: HTML page data must use inviteView instead of teams.Invite" >&2
	exit 1
fi
if grep -Eq '^[[:space:]]*Team[[:space:]]+teams\.Team([[:space:]]|$)|^[[:space:]]*Members[[:space:]]+\[\]teams\.Member([[:space:]]|$)' internal/web/*.go; then
	echo "architecture check: HTML page data must use teamView and memberView" >&2
	exit 1
fi

# HTTP request JSON must go through the shared bounded decoder. Direct
# json.Decoder use can accept trailing values and allocate from unbounded bodies.
for source in internal/web/*.go; do
	case "$source" in
		*_test.go) continue ;;
	esac
	if grep -Eq 'json\.NewDecoder\((r|req)\.Body\)' "$source"; then
		echo "architecture check: HTTP request JSON must use internal/httpjson.Decode ($source)" >&2
		exit 1
	fi
done

# Executable frontend behavior belongs in embedded JavaScript modules. The
# import map is the only template-local script block and contains data only.
for source in internal/web/templates/*.html; do
	if grep -Eiq '(^|[[:space:]])x-init[[:space:]]*=' "$source" ||
		grep -Eq 'x-data[[:space:]]*=[[:space:]]*"[[:space:]]*\{' "$source" ||
		grep -Eq "x-data[[:space:]]*=[[:space:]]*'[[:space:]]*\\{" "$source"; then
		echo "architecture check: Alpine state and initialization must live in named JS components ($source)" >&2
		exit 1
	fi
	if grep -Eiq '(^|[[:space:]])(@[[:alpha:]][[:alnum:]_-]*|x-on:[[:alpha:]][[:alnum:]_-]*|hx-on:[[:alpha:]][[:alnum:]_-]*|on[[:alpha:]][[:alnum:]_-]*)[[:space:]]*=' "$source"; then
		echo "architecture check: template event behavior must live in an embedded JS module ($source)" >&2
		exit 1
	fi
	script_tags=$(grep -Eo '<script[^>]*>' "$source" || true)
	while IFS= read -r tag; do
		[ -z "$tag" ] && continue
		case "$tag" in
			*src=*|*'type="importmap"'*) ;;
			*)
				echo "architecture check: executable script in $source must live in an embedded JS module ($tag)" >&2
				exit 1
				;;
		esac
	done <<EOF
$script_tags
EOF
done

# Browser modules share APIs through imports instead of mutable window slots.
# Reading browser or vendor globals remains allowed; modules cannot publish
# application state by assigning new properties on window/globalThis.
if grep -Eq '(window|globalThis)\.[A-Za-z_$][A-Za-z0-9_$]*[[:space:]]*=[^=]' internal/web/static/js/app*.js; then
	echo "architecture check: frontend modules must not publish mutable globals; use ES module imports/exports" >&2
	exit 1
fi
for source in internal/web/static/js/app*.js; do
	[ "$source" = "internal/web/static/js/app-offline.js" ] && continue
if grep -Eq '(^|[^[:alnum:]_])fetch[[:space:]]*\(' "$source"; then
		echo "architecture check: manual browser requests must use the shared offline-aware apiFetch ($source)" >&2
		exit 1
	fi
done

# Business-transition audit/event side effects live with their workflows, so
# alternate adapters cannot silently skip them or duplicate them. Keep these
# migrated actions out of HTTP handlers as more transports are added.
for action in \
	import.run invoice.create invoice.edit invoice.rebuild invoice.paylink invoice.send_queued activity.project \
	auth.login auth.logout auth.register auth.sso_login auth.sso_register auth.api_token_create auth.api_token_delete \
	integration.create integration.delete \
	payroll.create payroll.paid member.pay_update push.subscribe push.unsubscribe \
	session.start session.stop session.reopen session.delete \
	member.role team.billing team.currency team.stripe_update team.transfer \
	webhook.create webhook.delete; do
	if grep -Eq "s\\.audit\\(r, \\\"$action\\\"" internal/web/handlers_*.go; then
		echo "architecture check: $action audit belongs in its application workflow, not an HTTP handler" >&2
		exit 1
	fi
done
