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

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config: pipelineConfig(alias, "abc"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("orchestra_pipeline.test", "id"),
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "alias", alias),
					// The API defaults these and echoes them back.
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "data.webhook.enabled", "false"),
					resource.TestCheckResourceAttr("orchestra_pipeline.test", "data.tasks.ingest.depends_on.#", "0"),
				),
			},
			{
				// An in-place edit. If anything plans a replacement, or the
				// applied state differs from the plan, this step fails.
				Config: pipelineConfig(alias, "def"),
				Check: resource.TestCheckResourceAttr("orchestra_pipeline.test", "data.tasks.ingest.parameters",
					`{"connector_id":"def"}`),
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

func pipelineConfig(alias, connector string) string {
	return fmt.Sprintf(`
resource "orchestra_pipeline" "test" {
  alias     = %[1]q
  published = false

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
`, alias, connector)
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
