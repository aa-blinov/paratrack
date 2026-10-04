# Sourced by scripts/check-architecture.sh; do not execute directly.
# Apply dependency rules to every internal package, including packages added
# later. Only the process composition root may assemble concrete infrastructure;
# only the transport adapters may depend on the application service graph.
leaf_packages="appmodel catalog depcheck httpjson httpretry httpurl i18n mailport model money netclients oidcport netpolicy postcommit providerstatus pushport reportstats requestctx teamslug timeparse translit webhookport"
port_packages="cliport importport integrationport pushport webhookport"
adapter_packages="mail oidcclient stripe"
# Port-implementing subpackages need explicit exceptions to the generic
# workflow-import prohibition. Their owning workflows are enforced below;
# composition and transport packages remain forbidden.
port_adapter_packages="importproviders:importport integrations/providers:integrationport push/providers:pushport webhooks/httpdelivery:webhookport"
port_adapter_names="importproviders integrations/providers push/providers webhooks/httpdelivery"
special_packages="app cli db testutil web"
classified_packages="$core_packages $leaf_packages $port_packages $adapter_packages $port_adapter_names $special_packages"

# Provider and delivery contracts own their boundary data types. They must not
# grow a dependency on the shared product model and pull unrelated concepts in.
for name in importport integrationport mailport pushport webhookport; do
	check_no_direct_import "$module/internal/$name" internal/model
done

# A new package must be assigned an architectural role before its dependency
# rules can pass. Otherwise the default case below would leave it outside the
# workflow, leaf, and outbound-adapter boundaries.
 # Process settings and global facilities belong at explicit runtime roots.
# Keep environment reads in the binary (plus the integration-test DB helper),
# and wall-clock defaults in the process/runtime constructors. HTTP request
# duration measurement is the only request-time exception.
for source in $(find cmd internal -type f -name '*.go' ! -name '*_test.go'); do
	if grep -Eq 'os\.(Getenv|LookupEnv|Environ)\(' "$source"; then
		case "$source" in cmd/paratrack/config.go|internal/testutil/db.go) ;; *)
			echo "architecture check: process environment reads belong in the process config root ($source)" >&2
			exit 1
			;;
		esac
	fi
	if grep -Eq 'log\.(Default|Print|Printf|Println|Fatal|Fatalf|Fatalln)\(' "$source"; then
		case "$source" in cmd/paratrack/main.go|cmd/paratrack/config.go) ;; *)
			echo "architecture check: global logging belongs in the process root ($source)" >&2
			exit 1
			;;
		esac
	fi
	if grep -Eq 'time\.Now\(' "$source"; then
		case "$source" in
			cmd/paratrack/main.go|cmd/paratrack/config.go|internal/cli/runtime.go|internal/testutil/db.go|internal/web/request_logging.go) ;;
			*)
				echo "architecture check: wall-clock reads belong in runtime roots or elapsed-time instrumentation ($source)" >&2
				exit 1
				;;
		esac
	fi
done

for package in $package_paths; do
	case "$package" in "$module/internal/"*) ;; *) continue ;; esac
	name=${package#"$module/internal/"}
	case " $classified_packages " in
		*" $name "*) ;;
		*)
			echo "architecture check: classify new internal package $package as workflow, leaf, adapter, or special" >&2
			exit 1
			;;
	esac
done

# Outbound clients carry timeout, redirect, proxy, and network-address policy.
# Keep their construction in the shared client factories, except the isolated
# Sentry telemetry tunnel owned by the HTTP adapter.
for source in $(find internal -type f -name '*.go' ! -name '*_test.go'); do
	case "$source" in internal/netclients/clients.go|internal/web/sentry.go) continue ;; esac
	if grep -Eq '&http\.Client[[:space:]]*\{|http\.DefaultClient|http\.DefaultTransport|http\.(Get|Post|PostForm|Head)\(' "$source"; then
		echo "architecture check: $source must use an owned outbound client factory" >&2
		exit 1
	fi
done

for package in $package_paths; do
	case "$package" in "$module/internal/"*) ;; *) continue ;; esac
	case "$package" in
		"$module/internal/app")
			check_forbidden_imports "$package" internal/web internal/cli
			if grep -REq 'log\.Default\(|time\.Now\(' internal/app --include='*.go' --exclude='*_test.go' ||
				! grep -q 'validateConfig(config)' internal/app/services.go ||
				! grep -q 'validateCLIConfig(config)' internal/app/cli_services.go; then
				echo "architecture check: application composition must validate each graph's required dependencies" >&2
				exit 1
			fi
			cli_config=$(sed -n '/^type CLIConfig struct {/,/^}/p' internal/app/config.go)
			if ! printf '%s\n' "$cli_config" | grep -q 'Logger' || printf '%s\n' "$cli_config" | grep -q 'Now'; then
				echo "architecture check: CLIConfig must require only the CLI logger, not the server clock" >&2
				exit 1
			fi
			;;
		"$module/internal/web")
			forbidden="internal/db internal/cli internal/app internal/mail internal/stripe internal/push/providers internal/httpretry"
			for workflow in $core_packages; do
				[ "$workflow" = web ] && continue
				forbidden="$forbidden internal/$workflow"
			done
			check_forbidden_imports "$package" $forbidden
			;;
		"$module/internal/cli")
			forbidden="internal/db internal/web internal/app"
			for workflow in $core_packages; do
				forbidden="$forbidden internal/$workflow"
			done
			check_forbidden_imports "$package" $forbidden
			;;
		"$module/internal/db")
			forbidden="internal/web internal/cli internal/app internal/integrations/providers internal/netclients"
			for workflow in $core_packages; do
				forbidden="$forbidden internal/$workflow"
			done
			check_forbidden_imports "$package" $forbidden
			;;
		"$module/internal/testutil")
			# Test helpers may open the real persistence adapter, but are only
			# imported by _test.go files and must not become a runtime layer.
			check_forbidden_imports "$package" internal/web internal/cli internal/app
			;;
		*)
			check_forbidden_imports "$package" internal/db internal/web internal/cli internal/app
			;;
	esac
	if [ "$package" != "$module/internal/testutil" ]; then
		check_forbidden_imports "$package" internal/testutil
	fi
done

# Shared primitives and application contracts sit below feature workflows.
# They may depend on the domain model and standard/library support, but must
# not acquire product policy or adapter dependencies as the application grows.
for dependency in $(package_imports "$module/internal/model"); do
	case "$dependency" in
		"$module/internal/"*)
			echo "architecture check: shared model must not depend on internal package $dependency" >&2
			exit 1
			;;
esac
done

# Application requests and results stay independent of queue lease payloads;
# those retry contracts belong to the queue ports themselves. Provider output
# types remain valid appmodel inputs at the import and integration boundaries.
for dependency in $(package_imports "$module/internal/appmodel"); do
	case "$dependency" in
		"$module/internal/mailport"|"$module/internal/webhookport")
			echo "architecture check: appmodel must not depend on queue contract $dependency" >&2
			exit 1
			;;
	esac
done

# Durable delivery jobs and credential-bearing push endpoints belong to their
# feature ports, not to the shared domain model.
if grep -R -n -E --include='*.go' --exclude='*_test.go' \
	'^type (OutboundInvoiceEmail|OutboundWebhookDelivery|OutboundWebhookEvent|PushSubscription|WebhookDeliverySummary|WebhookSummary|Webhook) struct' internal/model; then
	echo "architecture check: mail, push, and webhook transport models belong in their feature-owned ports" >&2
	exit 1
fi

if grep -Eq '^[[:space:]]*ErrInvalid[A-Z]|^[[:space:]]*ErrIncompleteTaskSnapshot' internal/model/errors.go; then
	echo "architecture check: application validation and provider snapshot errors belong in appmodel, not shared model" >&2
	exit 1
fi

for name in $leaf_packages; do
	package="$module/internal/$name"
	[ -d "internal/$name" ] || continue
	forbidden="internal/db internal/web internal/cli internal/app"
	for workflow in $core_packages; do
		forbidden="$forbidden internal/$workflow"
	done
	# Word splitting passes the generated package list as function arguments.
	check_forbidden_imports "$package" $forbidden
done

# Standalone outbound adapters do not own workflow policy and must not reach
# upward into feature services. Provider adapters that implement a workflow
# port (for example integrations/providers) are deliberately handled apart.
for name in $adapter_packages; do
	package="$module/internal/$name"
	[ -d "internal/$name" ] || continue
	forbidden="internal/db internal/web internal/cli internal/app"
	for workflow in $core_packages; do
		forbidden="$forbidden internal/$workflow"
	done
	check_forbidden_imports "$package" $forbidden
done

# Port-implementing outbound adapters may depend on their own workflow's
# contract, but not on unrelated workflows or process/transport composition.
for adapter in $port_adapter_packages; do
	name=${adapter%%:*}
	owner=${adapter#*:}
	package="$module/internal/$name"
	forbidden="internal/db internal/web internal/cli internal/app"
	for workflow in $core_packages; do
		[ "$workflow" = "$owner" ] && continue
		forbidden="$forbidden internal/$workflow"
	done
	check_forbidden_imports "$package" $forbidden
done

# The CLI contract package defines adapter-facing ports. It must stay below
# feature workflows and concrete adapters so command dependencies remain
# substitutable and the interface bundle cannot become another service layer.
cli_contract="$module/internal/cliport"

if [ -d internal/cliport ]; then
	forbidden="internal/db internal/web internal/cli internal/app internal/mail internal/netclients internal/importproviders internal/integrations/providers"
	for workflow in $core_packages; do
		forbidden="$forbidden internal/$workflow"
	done
	check_forbidden_imports "$cli_contract" $forbidden
fi

# Workspace discovery and global project-ID lookups are local CLI adapter
# capabilities. Keep them out of the workspace-scoped project workflow.
for source in internal/projects/*.go; do
	case "$source" in *_test.go) continue ;; esac
	if grep -Eq 'FirstTeamID|TeamOwnerID|ProjectIDBySlug|GetProjectByID|DefaultTeam' "$source"; then
		echo "architecture check: CLI workspace resolution must stay outside the projects workflow ($source)" >&2
		exit 1
	fi
done
project_cli_contract=$(sed -n '/^type Projects interface {/,/^}/p' internal/cliport/services.go)
if printf '%s\n' "$project_cli_contract" | grep -Eq 'DefaultTeam|TeamOwnerID|ProjectIDBySlug|GetProjectByID'; then
	echo "architecture check: workspace and cross-workspace lookups belong to separate CLI ports" >&2
	exit 1
fi
workspace_cli_contract=$(sed -n '/^type WorkspaceContext interface {/,/^}/p' internal/cliport/services.go)
if printf '%s\n' "$workspace_cli_contract" | grep -Eq 'ProjectIDBySlug|GetProjectByID'; then
	echo "architecture check: cross-workspace lookups belong to the CLI ProjectLookup port" >&2
	exit 1
fi
project_lookup_contract=$(sed -n '/^type ProjectLookup interface {/,/^}/p' internal/cliport/services.go)
if ! printf '%s\n' "$project_lookup_contract" | grep -q 'ProjectIDBySlug' || ! printf '%s\n' "$project_lookup_contract" | grep -q 'GetProjectByID'; then
	echo "architecture check: CLI ProjectLookup must own global project lookup methods" >&2
	exit 1
fi
timer_cli_contract=$(sed -n '/^type TimerCommands interface {/,/^}/p' internal/cliport/services.go)
activity_cli_contract=$(sed -n '/^type ActivityCatalog interface {/,/^}/p' internal/cliport/services.go)
if printf '%s\n' "$timer_cli_contract" | grep -q 'ResolveActivityForMember' || ! printf '%s\n' "$activity_cli_contract" | grep -q 'ResolveActivityForMember'; then
	echo "architecture check: activity resolution belongs to the CLI ActivityCatalog port" >&2
	exit 1
fi

# Keep HTTP consumer ports split along the read, command and coordinated
# transition boundaries declared by their dependency groups.
http_port_contract() {
	sed -n "/^type $1 interface {/,/^}/p" internal/web/dependencies.go
}
http_port_has() {
	printf '%s\n' "$1" | grep -Eq "^[[:space:]]*$2\\("
}
check_http_port_split() {
	query_contract=$(http_port_contract "$1")
	command_contract=$(http_port_contract "$2")
	if ! http_port_has "$query_contract" "$3" || http_port_has "$query_contract" "$4" ||
		! http_port_has "$command_contract" "$4" || http_port_has "$command_contract" "$3"; then
		echo "architecture check: HTTP $1 and $2 must keep $3 and $4 in separate ports" >&2
		exit 1
	fi
}
check_http_port_split TrackingQueries TrackingCommands SessionHistoryPage Pause
check_http_port_split InvoiceQueries InvoiceDrafts Get CreateDraftWithOverlapCheck
check_http_port_split IntegrationQueries IntegrationCommands List ConnectAndSync
check_http_port_split TagQueries TagCommands ListWithCounts AttachForMember

# Shared domain and application types stay independent of transport and
# persistence encoders. Adapters own field mappings; json:"-" is permitted
# only as an explicit serialization denylist for sensitive/internal fields.
if grep -R -n -E --include='*.go' --exclude='*_test.go' '((db|sql|gorm):"|json:"[^-])' internal/model internal/appmodel internal/importport internal/integrationport; then
	echo "architecture check: transport and persistence tags belong in adapters, not shared types" >&2
	exit 1
fi

# CLI timer transitions with cross-cutting effects must use the same operation
# coordinator as HTTP. Reads and transitions without those effects remain on
# the narrower timer command port.
if grep -Eq 'services\.Timers\.(Start|Stop|StopAll)\(' internal/cli/*.go; then
	echo "architecture check: CLI start/stop transitions must use TimerOperations" >&2
	exit 1
fi
if grep -Eq 'services\.(TimerOperations|TrackingOps)\.Start\(' internal/cli/*.go internal/web/*.go; then
	echo "architecture check: named timer starts must use the shared StartActivity operation" >&2
	exit 1
fi
for operation in Stop StopAll; do
	if ! grep -Eq "services\\.TimerOperations\\.$operation\\(" internal/cli/*.go; then
		echo "architecture check: CLI must route $operation through TimerOperations" >&2
		exit 1
	fi
done
if ! grep -Eq 'services\.TimerOperations\.FocusActivityForMember\(' internal/cli/*.go; then
	echo "architecture check: CLI named focus must resolve and focus through TimerOperations" >&2
	exit 1
fi
if ! grep -Eq 'services\.TimerOperations\.StartActivity\(' internal/cli/*.go; then
	echo "architecture check: CLI must route named activity starts through TimerOperations" >&2
	exit 1
fi
if ! grep -Eq 'services\.TimerOperations\.AddClosed\(' internal/cli/*.go ||
	grep -Eq 'services\.Timers\.AddClosed\(' internal/cli/*.go; then
	echo "architecture check: CLI historical-session creation must use the audited TimerOperations workflow" >&2
	exit 1
fi
if grep -Eq 'services\.Tracking\.Focus\(' internal/web/*.go; then
	echo "architecture check: HTTP focus transitions must use TrackingOperations" >&2
	exit 1
fi
if ! grep -Eq 'services\.TrackingOps\.StartActivity\(' internal/web/handlers_timers.go; then
	echo "architecture check: HTTP named activity starts must use TrackingOperations" >&2
	exit 1
fi
if ! grep -Eq 'services\.TrackingOps\.StartActivity\(' internal/web/handlers_api_v1.go; then
	echo "architecture check: API v1 named activity starts must use TrackingOperations" >&2
	exit 1
fi
if ! grep -Eq 'services\.TrackingOps\.AddClosedActivity\(' internal/web/handlers_backfill.go ||
	grep -Eq 'services\.Tracking\.Commands\.(ResolveActivityForMember|AddClosed)\(' internal/web/handlers_backfill.go; then
	echo "architecture check: HTTP backfill activity resolution and persistence must use TrackingOperations" >&2
	exit 1
fi
if grep -Eq 'NewlyAchievedAfterSession\(' internal/web/handlers_*.go; then
	echo "architecture check: stop-triggered goal policy belongs in trackingops, not HTTP handlers" >&2
	exit 1
fi
for source in internal/trackingops/service.go internal/invoicing/service.go internal/invoicing/stripe.go \
	internal/billing/webhook.go internal/importing/importing.go; do
	if grep -q '\.Dispatch(' "$source"; then
		echo "architecture check: $source must not publish transactionally persisted events after commit" >&2
		exit 1
	fi
done
# Event fan-out and endpoint delivery need independent worker pools so a
# continuous event backlog cannot occupy every HTTP delivery worker.
webhook_run=$(sed -n '/^func (s \*Service) Run(/,/^}/p' internal/webhooks/delivery.go)
if ! printf '%s\n' "$webhook_run" | grep -q 'runEventWorker' ||
	! printf '%s\n' "$webhook_run" | grep -q 'runDeliveryWorker' ||
	! grep -q 'eventWorkerCount' internal/webhooks/service.go ||
	! grep -q 'deliveryWorkerCount' internal/webhooks/service.go; then
	echo "architecture check: webhook event fan-out and delivery must use separate worker pools" >&2
	exit 1
fi
stop_all_operation=$(sed -n '/^func (s \*Service) StopAll(/,/^}/p' internal/trackingops/service.go)
goal_notification_operation=$(sed -n '/^func (s \*Service) notifyGoalsAfterSessions(/,/^}/p' internal/trackingops/service.go)
if ! printf '%s\n' "$stop_all_operation" | grep -q 'notifyGoalsAfterSessions' ||
	! printf '%s\n' "$goal_notification_operation" | grep -q 'NewlyAchievedAfterSessions'; then
	echo "architecture check: bulk stop must evaluate goal progress from its complete session batch" >&2
	exit 1
fi

# Provider date defaults use the injected process clock. Avoid a helper that
# silently resolves a second wall clock outside the configured importer.
for source in $(find internal/importproviders -type f -name '*.go' ! -name '*_test.go'); do
	if grep -Eq 'time\.Now\(' "$source"; then
		echo "architecture check: provider date ranges must use the injected importer clock ($source)" >&2
		exit 1
	fi
done
