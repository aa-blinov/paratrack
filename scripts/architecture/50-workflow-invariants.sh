# Sourced by scripts/check-architecture.sh; do not execute directly.
# Core workflow packages depend on shared model/utility packages and on ports
# defined by their own package. They must not reach into another workflow's
# implementation; cross-workflow orchestration belongs in an explicit
# coordinator through narrow interfaces. Provider adapters remain outside this
# list and may implement a port exposed by their owning workflow.
for name in $core_packages; do
	package="$module/internal/$name"
	check_forbidden_imports "$package" internal/mail internal/netclients internal/importproviders internal/integrations/providers internal/stripe internal/push/providers
	imports=$(printf '%s' "$(package_imports "$package")" | tr ' ' ',')
	for source in internal/"$name"/*.go; do
		case "$source" in *_test.go) continue ;; esac
		if grep -Eq 'time\.(Now|Since|Until)\(' "$source"; then
			echo "architecture check: workflow source $source must use caller-supplied time or an injected clock" >&2
			exit 1
		fi
	done
	case "$name" in
		tracking|goals|tagging|projects)
			for source in internal/"$name"/*.go; do
				case "$source" in *_test.go) continue ;; esac
				if grep -Eq 'teamID[[:space:]]*(<|>|==)[[:space:]]*0' "$source"; then
					echo "architecture check: scoped workflow source $source must not use team ID 0 as an unscoped compatibility path" >&2
					exit 1
				fi
			done
			;;
	esac
	case ",$imports," in
		*,log,*)
			echo "architecture check: workflow package $package must receive a logger instead of using the global logger" >&2
			exit 1
			;;
	esac
	for peer in $core_packages; do
		if [ "$peer" = "$name" ]; then
			continue
		fi
		case ",$imports," in
			*,"$module/internal/$peer",*)
				echo "architecture check: $package must not import workflow package $module/internal/$peer" >&2
				exit 1
				;;
		esac
	done
done

# HTTP owns its consumer contracts while the composition graph owns concrete
# services. Keep concrete workflow implementations out of the HTTP bundle.
http_dependency_fields=$(sed -n '/^type Dependencies struct {/,/^}/p' internal/web/dependencies.go)
if printf '%s\n' "$http_dependency_fields" | grep -Eq '^[[:space:]]*[A-Z][A-Za-z0-9]*[[:space:]]+\*[^[:space:]]+\.Service([[:space:]]|$)'; then
	echo "architecture check: HTTP dependencies must use consumer interfaces instead of concrete services" >&2
	exit 1
fi
authentication_ports=
for port in IdentityWorkflow SignInWorkflow PasswordRecoveryWorkflow ProfileWorkflow APITokenWorkflow; do
	authentication_ports="$authentication_ports
$(sed -n "/^type $port interface {/,/^}/p" internal/web/dependencies.go)"
done
if printf '%s\n' "$authentication_ports" | grep -Eq '^[[:space:]]*(VerifyPassword|UpdatePassword)\('; then
	echo "architecture check: HTTP must request password changes through the auth workflow" >&2
	exit 1
fi
if printf '%s\n' "$authentication_ports" | grep -Eq 'FindByToken\('; then
	echo "architecture check: HTTP session validation must return a credential-free identity" >&2
	exit 1
fi
if printf '%s\n' "$authentication_ports" | grep -Eq 'model\.User([[:space:]]|[,)]|$)' ||
	! printf '%s\n' "$authentication_ports" | grep -q 'appmodel\.UserIdentity' ||
	grep -Eq 'PasswordHash|auth\.User' internal/web/*.go; then
	echo "architecture check: HTTP auth boundaries must use credential-free user identities" >&2
	exit 1
fi
if grep -Eq 'model\.(User|Integration|Webhook|PushSubscription|TeamInvite)([[:space:]]|[,)]|$)' internal/web/*.go; then
	echo "architecture check: HTTP adapters must not consume credential-bearing persistence models; use appmodel result types" >&2
	exit 1
fi
webhook_port=$(sed -n '/^type WebhookWorkflow interface {/,/^}/p' internal/web/dependencies.go)
if printf '%s\n' "$webhook_port" | grep -Eq 'model\.Webhook([[:space:]]|[,)]|$)'; then
	echo "architecture check: HTTP webhook boundaries must use credential-free summaries" >&2
	exit 1
fi
integration_port=$(sed -n '/^type IntegrationWorkflow interface {/,/^}/p' internal/web/dependencies.go)
if printf '%s\n' "$integration_port" | grep -Eq 'model\.Integration([[:space:]]|[,)]|$)'; then
	echo "architecture check: HTTP integration boundaries must use credential-free summaries" >&2
	exit 1
fi
push_port=$(sed -n '/^type PushWorkflow interface {/,/^}/p' internal/web/dependencies.go)
if printf '%s\n' "$push_port" | grep -Eq 'EnqueueNotification\('; then
	echo "architecture check: application push notifications must stay outside the HTTP consumer port" >&2
	exit 1
fi
if printf '%s\n' "$push_port" | grep -Eq 'Subscriptions\(|model\.PushSubscription'; then
	echo "architecture check: push delivery credentials must not cross the HTTP consumer port" >&2
	exit 1
fi
for port in PushWorkflow WebhookWorkflow InvoiceMailQueue; do
	worker_port=$(sed -n "/^type $port interface {/,/^}/p" internal/web/dependencies.go)
	if printf '%s\n' "$worker_port" | grep -Eq 'Run\(|Shutdown\('; then
		echo "architecture check: worker lifecycle does not belong in HTTP workflow port $port" >&2
		exit 1
	fi
done
tracking_commands=$(sed -n '/^type SessionStore interface {/,/^}/p' internal/tracking/service.go)
tracking_queries=$(sed -n '/^type SessionQueryStore interface {/,/^}/p' internal/tracking/service.go)
if printf '%s\n' "$tracking_commands" | grep -q 'GetSession(' || ! printf '%s\n' "$tracking_queries" | grep -q 'GetSession('; then
	echo "architecture check: tracking session reads belong in SessionQueryStore, separate from SessionStore commands" >&2
	exit 1
fi
if grep -Eq '(^|[^[:alnum:]_])StripeCredentials|\.TeamStripe\(|StripeAPIKey|StripeWebhookSecret|CreateCheckout\(|VerifyWebhookSignature\(' internal/web/*.go; then
	echo "architecture check: Stripe credentials and provider operations belong behind the invoicing workflow" >&2
	exit 1
fi
if grep -Eq 'github\.com/SherClockHolmes/webpush-go' internal/web/*.go; then
	echo "architecture check: Web Push protocol calls belong behind the push workflow port" >&2
	exit 1
fi
if grep -R -Eq 'fmt\.Errorf\("%w: %v"' internal --include='*.go' --exclude='*_test.go'; then
	echo "architecture check: wrapping errors must preserve both causes in the error chain" >&2
	exit 1
fi
for source in $(find internal -type f -name '*.go' ! -name '*_test.go'); do
	case "$source" in internal/postcommit/context.go) continue ;; esac
	if grep -Fq 'context.WithoutCancel(' "$source"; then
		echo "architecture check: detached effect contexts must use internal/postcommit ($source)" >&2
		exit 1
	fi
done
if grep -Eq 'CookieName|paratrack_session|net/http' internal/auth/*.go; then
	echo "architecture check: HTTP cookie names and protocol details belong in the web adapter" >&2
	exit 1
fi

# HTTP log output must use the server's configured logger so an embedding
# process can control routing, formatting and capture without global mutation.
for source in internal/web/*.go; do
	case "$source" in *_test.go) continue ;; esac
	if grep -Eq 'log\.Printf\(' "$source"; then
		echo "architecture check: HTTP runtime logging must use the configured server logger ($source)" >&2
		exit 1
	fi
done
if grep -Eq 'log\.Printf\(' internal/mail/*.go; then
	echo "architecture check: mail delivery logging must use its injected logger" >&2
	exit 1
fi
for source in internal/db/*.go; do
	case "$source" in *_test.go) continue ;; esac
	if grep -Eq 'log\.Printf\(' "$source"; then
		echo "architecture check: persistence logging must use its configured logger ($source)" >&2
		exit 1
	fi
done
