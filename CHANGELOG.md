# Changelog

## 0.1.0

First release of the Orchestra provider, published as `orchestra-hq/orchestra`.

### Provider

- `api_key` and `account_id`, falling back to the `ORCHESTRA_API_KEY` and `ORCHESTRA_ACCOUNT_ID` environment
  variables. Configuring the provider without a key fails with a clear message. `base_url` (or
  `ORCHESTRA_BASE_URL`) points it at another API host, and must use `https://`, since the key is sent with every
  request.
- Requests retry with backoff and honour `Retry-After`. A resource deleted outside Terraform is removed from state
  on the next refresh.
- API limits and enums are validated at plan time, so most invalid configurations fail at plan rather than in
  the middle of an apply.

### Resources

- `orchestra_pipeline`: a pipeline and its full definition in `data` (tasks, task groups, schedules, sensors,
  trigger events, alerts, anomalies, inputs and configuration), validated at plan time against the API's own
  schema. `paused` and `folder` (such as `finance/daily`) have their own endpoints, so changing only those does
  not save a new version, and either one left unset is left alone, so a pause or folder set in the Orchestra UI
  survives an apply. `published` chooses between publishing a change and saving it as a draft that leaves the
  published version running; an imported pipeline keeps the state it had in Orchestra. Changing `alias` replaces
  the pipeline. Pipelines whose definition lives in a git repository
  are not managed here; Orchestra's git sync owns them.
- `orchestra_environment`: a named set of values that pipeline runs resolve variables and credentials from.
  Leaving `default_env` unset keeps Orchestra's choice, which makes a workspace's first environment the default.
  Values are configuration rather than secrets (an `integration_credential` holds a connection ID), so changes
  to them show in plans.
- `orchestra_monitor`: which failures group into one incident and how it alerts. `match` picks the events a
  monitor claims, `group_by` and `group_by_labels` decide what makes two of them one incident, `trigger` makes it a
  threshold monitor, and `action` says what happens when one opens. A monitor update replaces the whole
  definition, so an optional field left out of the configuration, such as `action.subscribers`, is reset to its
  default and a value set in the Orchestra UI shows as drift. `order` is read-only.
- `orchestra_monitor_order`: puts the monitors it lists first, in that order, with every other monitor following
  in the order it already had. A monitor moved in front of the listed ones outside Terraform shows as drift, and
  one created during the apply is picked up rather than failing the reorder.
- `orchestra_incident_settings`: whether incidents are raised, which failures can raise one, and the alert
  budget. It is a workspace singleton, so settings left unset keep their current value and destroying it only
  removes it from state.
- `orchestra_integration_connection`: an integration's credentials, which tasks reference by ID. Secrets come back
  masked, so `parameters` is written but never refreshed; `parameters_wo` (Terraform 1.11+) keeps them out of
  state, with `parameters_wo_version` to send new values. Integrations that sign in through a browser (OAuth) are
  set up in the Orchestra UI instead.
- `orchestra_agent` and `orchestra_skill`: AI agents and the skills they use. A skill is markdown held in
  Terraform, or a file in a connected repository that Orchestra keeps in sync. An agent's `secrets` are written
  but never read back; `secrets_wo` (Terraform 1.11+) keeps them out of state, with `secrets_wo_version` to send
  new values.

### Data sources

- `orchestra_pipeline` and `orchestra_pipelines`
- `orchestra_environment` and `orchestra_environments`
- `orchestra_monitors`, in evaluation order, for importing or ordering monitors
- `orchestra_integration_connections`, to resolve the connection IDs that tasks reference
- `orchestra_assets`
- `orchestra_azure_federated_credentials`, for granting Orchestra access to Azure compute
- `orchestra_skills`, including the ones Orchestra publishes, so an agent can use them by ID

### Guides

- Creating a pipeline from an Orchestra pipeline YAML file
- Templating pipeline YAML with placeholders, loops and Orchestra's own `${{ }}` expressions
- Setting the connection each task runs with, including adopting a pipeline built in the Orchestra UI
- Connecting a dbt Core project from GitHub
