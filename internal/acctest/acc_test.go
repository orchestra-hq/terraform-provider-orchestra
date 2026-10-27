// Package acctest runs the provider against a real Orchestra workspace.
//
// These tests create and destroy real pipelines and environments. They are
// skipped unless TF_ACC=1 and ORCHESTRA_API_KEY are both set, which is the
// terraform-plugin-testing convention.
//
// They exist because everything else in this repo is checked against stub
// servers written from the spec, and the spec has been wrong: it declares
// GET /pipelines as a bare array (it returns an envelope when paginated) and
// its azure example uses subject_id (the API sends subjectId).
package acctest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/client"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/provider"
)

var protoV6 = map[string]func() (tfprotov6.ProviderServer, error){
	"orchestra": providerserver.NewProtocol6WithError(provider.New("acctest")()),
}

func preCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("ORCHESTRA_API_KEY") == "" {
		t.Skip("ORCHESTRA_API_KEY not set; skipping acceptance test")
	}
}

// TestAccPipeline_lifecycle is the test the unit suite cannot be: it proves a
// pipeline survives create, in-place update and refresh against the real API.
// A server-side default echoed into an Optional attribute fails here as
// "Provider produced inconsistent result after apply", and an attribute missing
// UseStateForUnknown shows up as a non-empty plan.
func TestAccPipeline_lifecycle(t *testing.T) {
	preCheck(t)
	alias := fmt.Sprintf("tf_acc_%d", os.Getpid())
	var versionBeforePause string
	const queue = `max_active = "1", on_limit = "QUEUE"`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config: pipelineConfig(alias, "abc", "", `max_active = "1"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("orchestra_pipeline.test", "id"),
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "alias", alias),
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "paused", "false"),
					// The API defaults these and echoes them back.
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "data.webhook.enabled", "false"),
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "data.tasks.ingest.depends_on.#", "0"),
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "data.configuration.concurrency.on_limit", "SKIP"),
				),
			},
			{
				// An in-place edit. If anything plans a replacement, or the
				// applied state differs from the plan, this step fails.
				Config: pipelineConfig(alias, "def", "", queue),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "data.tasks.ingest.parameters",
						`{"connector_id":"def"}`),
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "data.configuration.concurrency.on_limit", "QUEUE"),
					resource.TestCheckResourceAttrWith("orchestra_pipeline.test", "latest_version_number",
						func(v string) error { versionBeforePause = v; return nil }),
				),
			},
			{
				// Pausing alone goes through its own endpoint, not a new
				// version of the definition.
				Config: pipelineConfig(alias, "def", "paused = true", queue),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "paused", "true"),
					resource.TestCheckResourceAttrWith("orchestra_pipeline.test", "latest_version_number",
						func(v string) error {
							if v != versionBeforePause {
								return fmt.Errorf("pausing saved a new definition version: %s, was %s", v, versionBeforePause)
							}
							return nil
						}),
				),
			},
			{
				ResourceName:      "orchestra_pipeline.test",
				ImportState:       true,
				ImportStateId:     alias,
				ImportStateVerify: false, // data is fetched separately; compared by the next plan
			},
		},
	})
}

// TestAccPipeline_folder proves a folder survives create, a move and clearing
// against the real API. The API omits a folder it does not have, so a cleared
// one shows up here as a non-empty plan if `""` is not held in state.
func TestAccPipeline_folder(t *testing.T) {
	preCheck(t)
	alias := fmt.Sprintf("tf_acc_folder_%d", os.Getpid())
	folder := "tf_acc/" + alias
	var versionBeforeMove string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config: pipelineConfig(alias, "abc", fmt.Sprintf("folder = %q", folder), `max_active = "1"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "folder", folder),
					resource.TestCheckResourceAttrWith("orchestra_pipeline.test", "latest_version_number",
						func(v string) error { versionBeforeMove = v; return nil }),
				),
			},
			{
				Config: pipelineConfig(alias, "abc", fmt.Sprintf("folder = %q", folder+"/moved"), `max_active = "1"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "folder", folder+"/moved"),
					resource.TestCheckResourceAttrWith("orchestra_pipeline.test", "latest_version_number",
						func(v string) error {
							if v != versionBeforeMove {
								return fmt.Errorf("moving saved a new definition version: %s, was %s", v, versionBeforeMove)
							}
							return nil
						}),
				),
			},
			{
				Config: pipelineConfig(alias, "abc", `folder = ""`, `max_active = "1"`),
				Check:  resource.TestCheckResourceAttr("orchestra_pipeline.test", "folder", ""),
			},
		},
	})
}

func pipelineConfig(alias, connector, extra, concurrency string) string {
	return fmt.Sprintf(`
resource "orchestra_pipeline" "test" {
  alias     = %[1]q
  published = false
  %[3]s

  data = {
    name = "tf acceptance test"
    tasks = {
      ingest = {
        integration     = "FIVETRAN"
        integration_job = "FIVETRAN_SYNC_ALL"
        parameters      = jsonencode({ connector_id = %[2]q })
      }
    }
    configuration = { concurrency = { %[4]s } }
  }
}
`, alias, connector, extra, concurrency)
}

// TestAccPipelinesDataSource_envelope pins the pagination behaviour the spec
// gets wrong: GET /pipelines returns a {page,pageSize,total,results} envelope
// when paginated, not the bare array the spec declares.
func TestAccPipelinesDataSource_envelope(t *testing.T) {
	preCheck(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{{
			Config: `data "orchestra_pipelines" "all" {}`,
			Check:  resource.TestCheckResourceAttrSet("data.orchestra_pipelines.all", "pipelines.#"),
		}},
	})
}

// TestAccEnvironment_lifecycle covers the environment resource end to end.
// Environments carry credentials, so this also exercises the paths where a
// value could be normalised on the wire or echoed back differently.
func TestAccEnvironment_lifecycle(t *testing.T) {
	preCheck(t)
	skipIfEnvironmentLimitReached(t)
	name := fmt.Sprintf("tf_acc_env_%d", os.Getpid())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config: environmentConfig(name, "50"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("orchestra_environment.test", "id"),
					resource.TestCheckResourceAttr("orchestra_environment.test", "name", name),
					resource.TestCheckResourceAttr("orchestra_environment.test", "values.BATCH_SIZE.value", "50"),
					resource.TestCheckResourceAttr("orchestra_environment.test", "values.DEBUG.value", "true"),
					resource.TestCheckResourceAttr("orchestra_environment.test", "values.REGION.value", "eu-west-1"),
				),
			},
			{
				Config: environmentConfig(name, "100"),
				Check: resource.TestCheckResourceAttr("orchestra_environment.test",
					"values.BATCH_SIZE.value", "100"),
			},
			{
				ResourceName:      "orchestra_environment.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func environmentConfig(name, batchSize string) string {
	return fmt.Sprintf(`
resource "orchestra_environment" "test" {
  name = %[1]q
  values = {
    BATCH_SIZE = { type = "int", value = %[2]q }
    DEBUG      = { type = "bool", value = "true" }
    REGION     = { type = "string", value = "eu-west-1" }
  }
}
`, name, batchSize)
}

// skipIfEnvironmentLimitReached keeps this test honest on a workspace that
// cannot hold another environment. Orchestra caps environments per account, and
// a workspace already at the cap would otherwise fail here for a reason that
// has nothing to do with the provider.
func skipIfEnvironmentLimitReached(t *testing.T) {
	t.Helper()

	c := client.New(os.Getenv("ORCHESTRA_API_KEY"), os.Getenv("ORCHESTRA_BASE_URL"))
	probe := fmt.Sprintf("tf_acc_probe_%d", os.Getpid())

	var created struct {
		EnvironmentId string `json:"environmentId"`
	}
	err := c.DoJSON(context.Background(), http.MethodPost, "/environments", nil, map[string]any{
		"name":   probe,
		"values": map[string]any{},
	}, &created)
	if err != nil {
		if strings.Contains(err.Error(), "limit reached") {
			t.Skip("account is at its environment limit; skipping environment acceptance test")
		}
		t.Fatalf("probing the environments endpoint failed: %v", err)
	}

	// The probe succeeded, so the cap is not the issue. Remove it before the
	// real test runs, or it consumes the slot the test needs.
	if created.EnvironmentId != "" {
		if err := c.DoJSON(context.Background(), http.MethodDelete,
			"/environments/"+created.EnvironmentId, nil, nil, nil); err != nil {
			t.Fatalf("could not clean up probe environment %s: %v", created.EnvironmentId, err)
		}
	}
}

// TestAccMonitor_lifecycle covers the monitor resource against the real API.
// Monitors carry server-assigned `order` and several defaulted booleans, which
// is the shape that has broken applies before.
func TestAccMonitor_lifecycle(t *testing.T) {
	preCheck(t)
	name := fmt.Sprintf("tf_acc_monitor_%d", os.Getpid())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config: monitorConfig(name, "HIGH", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("orchestra_monitor.test", "id"),
					resource.TestCheckResourceAttrSet("orchestra_monitor.test", "order"),
					resource.TestCheckResourceAttr("orchestra_monitor.test", "name", name),
					resource.TestCheckResourceAttr("orchestra_monitor.test", "match.operation_failures_only", "true"),
					// Defaults the API fills in.
					resource.TestCheckResourceAttr("orchestra_monitor.test", "enabled", "true"),
					resource.TestCheckResourceAttr("orchestra_monitor.test", "action.mute", "false"),
					// Unset, so the provider sends [] and the API must keep it.
					resource.TestCheckResourceAttr("orchestra_monitor.test", "action.subscribers.#", "0"),
				),
			},
			{
				// Turning it off must store false, not keep the true already there.
				Config: monitorConfig(name, "MEDIUM", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("orchestra_monitor.test", "action.severity", "MEDIUM"),
					resource.TestCheckResourceAttr("orchestra_monitor.test", "match.operation_failures_only", "false"),
				),
			},
			{
				ResourceName:      "orchestra_monitor.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func monitorConfig(name, severity string, operationFailuresOnly bool) string {
	return fmt.Sprintf(`
resource "orchestra_monitor" "test" {
  name     = %[1]q
  match    = { integrations = ["SNOWFLAKE"], operation_failures_only = %[3]t }
  group_by = ["pipeline"]

  action = {
    severity = %[2]q
  }
}
`, name, severity, operationFailuresOnly)
}

// TestAccIntegrationConnection_lifecycle uses the HTTP integration: it needs no
// real account to authenticate against, and its bearer token is a secret, so
// the masked read-back is exercised too.
func TestAccIntegrationConnection_lifecycle(t *testing.T) {
	preCheck(t)
	name := fmt.Sprintf("tf_acc_conn_%d", os.Getpid())
	const addr = "orchestra_integration_connection.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config: connectionConfig(name, "tf-acc-token-1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(addr, "id"),
					resource.TestCheckResourceAttr(addr, "name", name),
					resource.TestCheckResourceAttrSet(addr, "credential_store"),
					resource.TestCheckResourceAttrSet(addr, "is_default"),
					resource.TestCheckResourceAttrSet(addr, "auth_status"),
				),
			},
			{
				// A rename and new credentials are two PATCHes.
				Config: connectionConfig(name+"_renamed", "tf-acc-token-2"),
				Check:  resource.TestCheckResourceAttr(addr, "name", name+"_renamed"),
			},
			{
				ResourceName: addr,
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return "HTTP/" + s.RootModule().Resources[addr].Primary.ID, nil
				},
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"parameters"}, // secrets come back masked
			},
		},
	})
}

func connectionConfig(name, token string) string {
	return fmt.Sprintf(`
resource "orchestra_integration_connection" "test" {
  integration = "HTTP"
  name        = %[1]q
  parameters = jsonencode({
    base_url     = "https://example.com"
    auth_type    = "BEARER_TOKEN"
    bearer_token = %[2]q
  })
}
`, name, token)
}

// TestAccIncidentSettings_lifecycle changes the workspace's one set of incident
// settings, so it puts the originals back afterwards: destroying the resource
// deliberately leaves the settings as they are.
func TestAccIncidentSettings_lifecycle(t *testing.T) {
	preCheck(t)
	ctx := context.Background()
	c := client.New(os.Getenv("ORCHESTRA_API_KEY"), os.Getenv("ORCHESTRA_BASE_URL"))

	var original map[string]any
	if err := c.DoJSON(ctx, http.MethodGet, "/incident_settings", nil, nil, &original); err != nil {
		t.Fatalf("reading the current incident settings: %v", err)
	}
	t.Cleanup(func() {
		restore := map[string]any{}
		for k, v := range original {
			if k != "updatedAt" { // read-only
				restore[k] = v
			}
		}
		if err := c.DoJSON(ctx, http.MethodPatch, "/incident_settings", nil, restore, nil); err != nil {
			t.Errorf("could not restore the workspace's incident settings: %v", err)
		}
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config: `
resource "orchestra_incident_settings" "test" {
  max_alerts_per_incident                     = 7
  alert_budget_refill_cadence                 = "weekly"
  auto_resolve_on_success                     = true
  require_default_branch_or_published_version = false
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("orchestra_incident_settings.test", "id", "incident_settings"),
					resource.TestCheckResourceAttr("orchestra_incident_settings.test", "max_alerts_per_incident", "7"),
					resource.TestCheckResourceAttr("orchestra_incident_settings.test", "auto_resolve_on_success", "true"),
					resource.TestCheckResourceAttr("orchestra_incident_settings.test", "require_default_branch_or_published_version", "false"),
					// Unset settings are read back, not left unknown.
					resource.TestCheckResourceAttrSet("orchestra_incident_settings.test", "incidents_enabled"),
					resource.TestCheckResourceAttrSet("orchestra_incident_settings.test", "require_alert_configured"),
				),
			},
			{
				Config: `
resource "orchestra_incident_settings" "test" {
  max_alerts_per_incident     = 9
  alert_budget_refill_cadence = "daily"
  environment_ids             = []
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("orchestra_incident_settings.test", "max_alerts_per_incident", "9"),
					resource.TestCheckResourceAttr("orchestra_incident_settings.test", "alert_budget_refill_cadence", "daily"),
					resource.TestCheckResourceAttr("orchestra_incident_settings.test", "environment_ids.#", "0"),
					// No longer configured, so left as it was.
					resource.TestCheckResourceAttr("orchestra_incident_settings.test", "auto_resolve_on_success", "true"),
				),
			},
			{
				ResourceName:      "orchestra_incident_settings.test",
				ImportState:       true,
				ImportStateId:     "incident_settings",
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccMonitorOrder_lifecycle orders two monitors, then drops one from both
// the configuration and the order in a single apply. Terraform updates the
// order before it destroys the monitor, and the API requires a reorder to name
// every monitor that exists, so this is the step a naive implementation fails.
func TestAccMonitorOrder_lifecycle(t *testing.T) {
	preCheck(t)
	prefix := fmt.Sprintf("tf_acc_order_%d", os.Getpid())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config: monitorOrderConfig(prefix, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("orchestra_monitor_order.test", "monitor_ids.#", "2"),
					resource.TestCheckResourceAttrPair("orchestra_monitor_order.test", "monitor_ids.0", "orchestra_monitor.b", "id"),
					resource.TestCheckResourceAttrPair("orchestra_monitor_order.test", "monitor_ids.1", "orchestra_monitor.a", "id"),
				),
			},
			{
				Config: monitorOrderConfig(prefix, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("orchestra_monitor_order.test", "monitor_ids.#", "1"),
					resource.TestCheckResourceAttrPair("orchestra_monitor_order.test", "monitor_ids.0", "orchestra_monitor.a", "id"),
					resource.TestCheckResourceAttrPair("data.orchestra_monitors.all", "monitors.0.id", "orchestra_monitor.a", "id"),
				),
			},
		},
	})
}

func monitorOrderConfig(prefix string, withB bool) string {
	monitor := func(name string) string {
		return fmt.Sprintf(`
resource "orchestra_monitor" %[1]q {
  name     = "%[2]s_%[1]s"
  match    = { integrations = ["SNOWFLAKE"] }
  group_by = ["pipeline"]
}
`, name, prefix)
	}
	if withB {
		return monitor("a") + monitor("b") + `
resource "orchestra_monitor_order" "test" {
  monitor_ids = [orchestra_monitor.b.id, orchestra_monitor.a.id]
}`
	}
	return monitor("a") + `
resource "orchestra_monitor_order" "test" {
  monitor_ids = [orchestra_monitor.a.id]
}

data "orchestra_monitors" "all" {
  depends_on = [orchestra_monitor_order.test]
}`
}

// TestAccAgent_lifecycle covers an agent and the skill it uses against the
// Agents API, which has its own base URL. Clearing the description checks that
// an explicit null clears a field rather than being ignored, and refreshing a
// skill checks that the API hands its document back unchanged.
func TestAccAgent_lifecycle(t *testing.T) {
	preCheck(t)
	skipWithoutAgents(t)
	suffix := fmt.Sprintf("%d", os.Getpid())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config: agentConfig(suffix, "Say hello.", `description = "tf acceptance test"`, "[orchestra_skill.test.id]"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("orchestra_agent.test", "id"),
					resource.TestCheckResourceAttr("orchestra_agent.test", "llm_key.type", "ACCOUNT_DEFAULT"),
					resource.TestCheckResourceAttr("orchestra_agent.test", "skill_ids.#", "1"),
					resource.TestCheckResourceAttr("orchestra_skill.test", "name", "tf-acc-"+suffix),
				),
			},
			{
				Config: agentConfig(suffix, "Say goodbye.", "", "[]"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("orchestra_agent.test", "description"),
					resource.TestCheckResourceAttr("orchestra_agent.test", "skill_ids.#", "0"),
				),
			},
			{
				ResourceName:            "orchestra_agent.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"secrets"}, // never read back
			},
			{
				ResourceName:      "orchestra_skill.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func agentConfig(suffix, instructions, description, skills string) string {
	return fmt.Sprintf(`
resource "orchestra_skill" "test" {
  markdown = <<-EOT
    ---
    name: tf-acc-%[1]s
    description: Terraform acceptance test skill
    ---

    %[2]s
  EOT
}

resource "orchestra_agent" "test" {
  name           = "tf_acc_agent_%[1]s"
  %[3]s
  sample_prompts = ["What failed last night?"]
  skill_ids      = %[4]s
  secrets        = { TF_ACC = "not-a-real-secret" }
}
`, suffix, instructions, description, skills)
}

// skipWithoutAgents skips on a workspace that has no access to the Agents API.
// Only a 403 counts: a 404 means the provider has the wrong base URL, which is
// exactly what this test is for.
func skipWithoutAgents(t *testing.T) {
	t.Helper()
	c := client.New(os.Getenv("ORCHESTRA_API_KEY"), os.Getenv("ORCHESTRA_BASE_URL")).Agents()
	err := c.DoJSON(context.Background(), http.MethodGet, "/skills", nil, nil, nil)
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
		t.Skipf("the Agents API is not available to this workspace: %v", err)
	}
	if err != nil {
		t.Fatalf("probing the Agents API: %v", err)
	}
}
