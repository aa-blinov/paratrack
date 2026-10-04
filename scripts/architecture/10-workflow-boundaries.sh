# Sourced by scripts/check-architecture.sh; do not execute directly.
# Outbound webhook event names are an external contract shared by the
# transactional persistence producers and endpoint validation.
for source in \
	internal/importing/importing.go \
	internal/invoicing/service.go internal/invoicing/stripe.go \
	internal/trackingops/service.go; do
	if grep -Eq '"(session\.(started|stopped)|invoice\.(created|payment_link_created)|import\.completed)"' "$source"; then
		echo "architecture check: $source must use webhookport event names" >&2
		exit 1
	fi
done

# Transport adapters consume application ports; they do not wire concrete
# graphs or reach into persistence directly.
# When an error is part of a neutral consumer contract, the transport maps
# that contract instead of importing its workflow implementation.
check_forbidden_imports "$module/internal/web" \
	internal/db internal/app internal/cli internal/netclients internal/httpretry \
	internal/importing internal/importproviders internal/integrations/providers internal/webhooks internal/webhooks/httpdelivery \
	internal/oidcclient internal/mail internal/stripe internal/push/providers
check_forbidden_imports "$module/internal/cli" \
	internal/db internal/app internal/web internal/netclients internal/httpretry \
	internal/importproviders internal/integrations/providers internal/webhooks/httpdelivery \
	internal/oidcclient internal/mail internal/stripe internal/push/providers
check_no_direct_import "$module/internal/web" internal/tracking
check_no_direct_import "$module/internal/cli" internal/tracking
check_no_direct_import "$module/internal/web" internal/projects
check_no_direct_import "$module/internal/cli" internal/projects
check_no_direct_import "$module/internal/web" internal/billing
check_no_direct_import "$module/internal/cli" internal/billing
check_no_direct_import "$module/internal/cli" internal/integrations

# The composition root may depend on workflows and infrastructure adapters,
# but transport adapters must never become construction dependencies.
check_forbidden_imports "$module/internal/app" internal/web internal/cli

# Shared domain values sit at the bottom of the dependency graph. They must
# not acquire dependencies on application or infrastructure packages.
if grep -Eq '^type [[:alnum:]_]+(Request|Command|Result) struct|^type (ProjectUpdate|SessionUpdate|InvoiceOptions|TokenOptions|ImportedEntry|ExternalTaskInput|InvoiceEmailEnqueue) struct' internal/model/*.go; then
	echo "architecture check: application and provider input types belong in internal/appmodel" >&2
	exit 1
fi
for dependency in $(package_deps "$module/internal/model"); do
	case "$dependency" in
		"$module/internal/"*)
			echo "architecture check: shared model must not depend on internal package $dependency" >&2
			exit 1
			;;
	esac
done

# Feature workflows depend on ports and transport-neutral data. Keep their
# dependency direction inward: adapters and composition belong outside them,
# and persistence must not import application policy back from a workflow.
# projectpages composes read-only workflow ports into a project-page snapshot.
core_packages="audit auth billing dashboard goals importing integrations integrationtracking invoicedocuments invoicing mailqueue memberadmin payroll payrollops preferences projectpages projects push reports scheduling sessiondecorations tagging teamops teams tokenadmin tracking trackingops webhooks"
for package in $core_packages; do
	import_path="$module/internal/$package"
	[ -d "internal/$package" ] || continue
	# Inspect the full graph so an intermediate shared package cannot hide
	# an adapter or protocol dependency from this boundary check.
	for dependency in $(package_deps "$import_path"); do
		case "$dependency" in
			"$module/internal/web"|"$module/internal/cli"|"$module/internal/app"|"$module/internal/db"|"$module/internal/cliport")
				echo "architecture check: workflow $import_path must not depend on adapter/composition package $dependency" >&2
				exit 1
				;;
			"$module/internal/netclients"|"$module/internal/httpretry"|"$module/internal/httpjson"|"$module/internal/httpurl"|"$module/internal/stripe"|"$module/internal/oidcclient"|"$module/internal/integrations/providers"|"$module/internal/importproviders"|"$module/internal/webhooks/httpdelivery"|"$module/internal/push/providers"|"$module/internal/mail")
				echo "architecture check: workflow $import_path must depend on an outbound port, not adapter $dependency" >&2
				exit 1
				;;
		esac
		if [ "$dependency" = net/http ]; then
			echo "architecture check: workflow $import_path must keep HTTP protocol details in an outbound adapter" >&2
			exit 1
		fi
		if [ "$dependency" = database/sql ] || [ "$dependency" = os/exec ]; then
			echo "architecture check: workflow $import_path must not depend on SQL or process execution APIs" >&2
			exit 1
		fi
		if [ "$dependency" = "$module/internal/netpolicy" ]; then
			echo "architecture check: workflow $import_path must keep network policy in an outbound adapter" >&2
			exit 1
		fi
	done
done

# Business workflows receive time through requests or injected clocks. Keep
# wall-clock reads in adapters and process composition so transitions remain
# deterministic and ordered under test.
for package in $core_packages; do
	[ "$package" = web ] && continue
	directory="internal/$package"
	[ -d "$directory" ] || continue
	for source in $(find "$directory" -type f -name '*.go' ! -name '*_test.go'); do
		if grep -Eq 'time\.Now\(' "$source"; then
			echo "architecture check: workflow $package must receive time through a request or injected clock ($source)" >&2
			exit 1
		fi
	done
done

# Feature workflows are composed together by internal/app. Depending on
# another workflow couples policy packages and bypasses the coordinating layer;
# check the transitive graph, excluding each package from its own dependency set.
for package in $core_packages; do
	[ "$package" = web ] && continue
	import_path="$module/internal/$package"
	[ -d "internal/$package" ] || continue
	for dependency in $(package_deps "$import_path"); do
		[ "$dependency" = "$import_path" ] && continue
		case " $core_packages " in
			*" ${dependency#"$module/internal/"} "*)
				echo "architecture check: workflow $import_path must not import another feature workflow $dependency" >&2
				exit 1
				;;
		esac
	done
done

for dependency in $(package_deps "$module/internal/db"); do
	[ "$dependency" = "$module/internal/db" ] && continue
	case " $core_packages " in
		*" ${dependency#"$module/internal/"} "*)
			echo "architecture check: persistence adapter must not depend on workflow $dependency" >&2
			exit 1
			;;
	esac
done
