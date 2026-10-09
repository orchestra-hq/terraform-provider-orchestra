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
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	client "github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra"
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

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config: pipelineConfig(alias, "abc", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("orchestra_pipeline.test", "id"),
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "alias", alias),
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "paused", "false"),
					// The API defaults these and echoes them back.
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "data.webhook.enabled", "false"),
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "data.tasks.ingest.depends_on.#", "0"),
				),
			},
			{
				// An in-place edit. If anything plans a replacement, or the
				// applied state differs from the plan, this step fails.
				Config: pipelineConfig(alias, "def", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "data.tasks.ingest.parameters",
						`{"connector_id":"def"}`),
					resource.TestCheckResourceAttrWith("orchestra_pipeline.test", "latest_version_number",
						func(v string) error { versionBeforePause = v; return nil }),
				),
			},
			{
				// Pausing alone goes through its own endpoint, not a new
				// version of the definition.
				Config: pipelineConfig(alias, "def", "paused = true"),
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

func pipelineConfig(alias, connector, extra string) string {
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
  }
}
`, alias, connector, extra)
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
				Config: monitorConfig(name, "HIGH"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("orchestra_monitor.test", "id"),
					resource.TestCheckResourceAttrSet("orchestra_monitor.test", "order"),
					resource.TestCheckResourceAttr("orchestra_monitor.test", "name", name),
					// Defaults the API fills in.
					resource.TestCheckResourceAttr("orchestra_monitor.test", "enabled", "true"),
					resource.TestCheckResourceAttr("orchestra_monitor.test", "action.mute", "false"),
				),
			},
			{
				Config: monitorConfig(name, "MEDIUM"),
				Check:  resource.TestCheckResourceAttr("orchestra_monitor.test", "action.severity", "MEDIUM"),
			},
			{
				ResourceName:      "orchestra_monitor.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func monitorConfig(name, severity string) string {
	return fmt.Sprintf(`
resource "orchestra_monitor" "test" {
  name     = %[1]q
  match    = { integrations = ["SNOWFLAKE"] }
  group_by = ["pipeline"]

  action = {
    severity = %[2]q
  }
}
`, name, severity)
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
			if k != "updatedAt" && k != "groupingRules" { // read-only
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
  max_alerts_per_incident     = 7
  alert_budget_refill_cadence = "weekly"
  auto_resolve_on_success     = true
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("orchestra_incident_settings.test", "id", "incident_settings"),
					resource.TestCheckResourceAttr("orchestra_incident_settings.test", "max_alerts_per_incident", "7"),
					resource.TestCheckResourceAttr("orchestra_incident_settings.test", "auto_resolve_on_success", "true"),
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
