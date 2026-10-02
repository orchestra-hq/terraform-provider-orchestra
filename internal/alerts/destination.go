// Package alerts holds the alert destination, shared by the pipeline
// definition's `alerts` and `anomalies` blocks and by a monitor's `action`.
//
// All three are the same `AlertDestinationModel` in the OpenAPI spec, so they
// share one schema, one set of validators and one conversion — a destination
// that is valid in a pipeline alert is valid in a monitor.
package alerts

import (
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/enums"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/tfconv"
)

// DestinationTF is the Terraform model for one alert destination.
//
// `priority` and `body` are flattened out of the spec's nested
// AlertParametersModel: it is an anyOf of the Datadog and webhook parameter
// models, whose fields are disjoint, so one flat object round-trips both.
type DestinationTF struct {
	Integration  types.String         `tfsdk:"integration"`
	Destination  types.String         `tfsdk:"destination"`
	ConnectionID types.String         `tfsdk:"connection_id"`
	Priority     types.Int64          `tfsdk:"priority"`
	Body         jsontypes.Normalized `tfsdk:"body"`
}

// WireDestination is components.schemas.AlertDestinationModel.
type WireDestination struct {
	Integration  string          `json:"integration"`
	Destination  *string         `json:"destination,omitempty"`
	ConnectionID *string         `json:"connectionId,omitempty"`
	Parameters   *WireParameters `json:"parameters,omitempty"`
}

// WireParameters is the anyOf of BaseDatadogNotificationParametersModel and
// WebhookAlertParametersModel.
type WireParameters struct {
	Priority *int64          `json:"priority,omitempty"`
	Body     json.RawMessage `json:"body,omitempty"`
}

// DestinationSchema is the nested object for a list of destinations.
func DestinationSchema() schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"integration": schema.StringAttribute{
				Required:    true,
				Description: "Notification integration to send through.",
				Validators:  []validator.String{stringvalidator.OneOf(enums.NotificationTypes...)},
			},
			"destination": schema.StringAttribute{
				Optional:            true,
				Description:         "Where to deliver: an email address, Slack channel, or webhook URL.",
				MarkdownDescription: "Where to deliver: an email address, Slack channel, or webhook URL, depending on `integration`.",
			},
			"connection_id": schema.StringAttribute{
				Optional:    true,
				Description: "Connection to authenticate the delivery with.",
			},
			"priority": schema.Int64Attribute{
				Optional:            true,
				Description:         "Datadog notification priority (1-5). Only valid when integration is DATADOG.",
				MarkdownDescription: "Datadog notification priority (1-5). Only valid when `integration = \"DATADOG\"`.",
			},
			"body": schema.StringAttribute{
				Optional:            true,
				CustomType:          jsontypes.NormalizedType{},
				Description:         "Webhook request body, JSON-encoded. Only valid when integration is WEBHOOK.",
				MarkdownDescription: "Webhook request body, JSON-encoded. Only valid when `integration = \"WEBHOOK\"`. The API accepts an object or a string, so encode either — `jsonencode({...})` or `jsonencode(\"text\")`.",
			},
		},
	}
}

// ToWire converts configured destinations for the API.
func ToWire(dests []DestinationTF, p path.Path, diags *diag.Diagnostics) []WireDestination {
	if len(dests) == 0 {
		return nil
	}
	out := make([]WireDestination, 0, len(dests))
	for i, d := range dests {
		wd := WireDestination{
			Integration:  d.Integration.ValueString(),
			Destination:  tfconv.StringPtrOrNil(d.Destination),
			ConnectionID: tfconv.StringPtrOrNil(d.ConnectionID),
		}
		priority := tfconv.Int64PtrOrNil(d.Priority)
		body := tfconv.JSONAny(d.Body, p.AtListIndex(i).AtName("body"), diags)
		if priority != nil || body != nil {
			wd.Parameters = &WireParameters{Priority: priority, Body: body}
		}
		out = append(out, wd)
	}
	return out
}

// FromWire is the inverse of ToWire.
func FromWire(dests []WireDestination) []DestinationTF {
	if len(dests) == 0 {
		return nil
	}
	out := make([]DestinationTF, 0, len(dests))
	for _, d := range dests {
		td := DestinationTF{
			Integration:  types.StringValue(d.Integration),
			Destination:  tfconv.String(d.Destination),
			ConnectionID: tfconv.String(d.ConnectionID),
			Priority:     types.Int64Null(),
			Body:         jsontypes.NewNormalizedNull(),
		}
		if d.Parameters != nil {
			td.Priority = tfconv.Int64(d.Parameters.Priority)
			td.Body = tfconv.RawJSON(d.Parameters.Body)
		}
		out = append(out, td)
	}
	return out
}
