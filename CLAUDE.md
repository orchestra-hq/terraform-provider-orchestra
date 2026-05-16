# Orchestra Docs — Agent Instructions

## Project Overview

This is the Orchestra documentation site, built with **Mintlify** (Aspen theme). All content lives in `/docs/`, config in `docs.json`. Run locally with `yarn mint dev`, lint with `yarn lint`.

---

## Source of Truth: Reading Changes to Document

Documentation changes are driven by code changes in a separate repository:

**`https://github.com/orchestra-hq/pipeline-service`**

This is the backend service repo where new features, integrations, and configuration changes are implemented. When writing or updating docs, always read from that repo first to understand what has changed — do not invent behaviour or guess at configuration based on the docs repo alone.

### Workflow for documenting a change

1. **Read the source repo.** Use `gh` or `WebFetch` to inspect the relevant PR, commit, or files in `orchestra-hq/pipeline-service`. Look at:
   - The PR description and linked issue for intent and user-facing summary
   - Changed source files for exact parameter names, types, defaults, and behaviour
   - Any new YAML schema files, Pydantic models, or enums that define config structure
   - Test files — they often show concrete usage examples

2. **Map code to docs.** Identify which existing doc page(s) the change affects, or whether a new page is needed. Cross-reference with the current `/docs/` structure and `docs.json` navigation.

3. **Write the docs.** Follow all conventions in this file. Use exact parameter names and types from the source code — do not paraphrase or normalise them.

4. **Commit only to this repo.** All edits are written here (`orchestra-hq/orchestra-docs`). Never attempt to push changes back to `pipeline-service`.

### Useful `gh` commands for reading the source repo

```bash
# View a PR in the source repo
gh pr view <number> --repo orchestra-hq/pipeline-service

# List recent merged PRs
gh pr list --repo orchestra-hq/pipeline-service --state merged --limit 20

# View files changed in a PR
gh pr diff <number> --repo orchestra-hq/pipeline-service

# View a specific file at HEAD
gh api repos/orchestra-hq/pipeline-service/contents/<path> --jq '.content' | base64 -d

# Search for a symbol or config key across the repo
gh search code <term> --repo orchestra-hq/pipeline-service
```

---

## File Conventions

- All doc files must be **`.mdx`** — never `.md`. Mintlify requires `.mdx` for JSX component support.
- Images go in `/static/img/`, organized loosely by feature area.
- New pages must be added to the `navigation.groups` array in `docs.json` to appear in the sidebar.
- Directory index pages are named `index.mdx` and typically contain card-based overviews of child pages.

---

## Frontmatter

Every page requires frontmatter. Use these fields:

```yaml
---
title: "Page Title"           # Required — shown in browser tab and sidebar
sidebarTitle: "Short Label"   # Optional — use when title is long
description: "1–2 sentence summary of the page."  # Required for SEO
---
```

Optional fields:

- `mode: "wide"` — full-width layout, used on overview/card pages
- `keywords: [keyword1, keyword2]` — improves search discoverability

---

## Writing Style

- **Active voice.** "Configure the pipeline" not "The pipeline can be configured."
- **Task-oriented.** Lead with what the user is trying to do.
- **Concise.** One idea per sentence. No filler preamble.
- **Internal links.** Always use descriptive link text — never `[here](url)`. Format: `[Pipeline overview](/docs/core-concepts/pipelines/)`.
- **Technical terms.** Bold on first use. Use inline code for config keys, IDs, CLI commands, and expression syntax.

### Page Structure Pattern

1. Short intro paragraph (1–2 sentences) explaining what this is and why it matters
2. `##` sections for main concepts or steps
3. YAML or code examples where applicable
4. FAQ section using `<AccordionGroup>` if there are common questions
5. "Next steps" or related links at the bottom

---

## Components

Use Mintlify MDX components. Never use raw HTML equivalents.

### Images — always use `<Frame>`

```mdx
<Frame caption="Descriptive caption explaining what the image shows.">
  <img src="/static/img/path/to/image.png" alt="Alt text describing the content, not just 'screenshot'"/>
</Frame>
```

- Captions and alt text are **required**.
- Optional width: add `width="80%"` or `maxWidth="24rem"` to the `<img>` tag when needed.

### Callouts

```mdx
<Info>
**Bold heading if needed** Explanatory text for neutral info or recommendations.
</Info>

<Tip>
Best practice or helpful shortcut.
</Tip>

<Warning>
Important caveat or potential gotcha.
</Warning>
```

### Overview / Card Pages

Used on index pages for a section. Combine with `mode: "wide"` in frontmatter.

```mdx
<Columns cols={2}>
  <Card title="Card Title" href="/docs/path/to/page" icon="icon-name" horizontal>
    Short description of what's on this page.
  </Card>
</Columns>
```

### Tabs

```mdx
<Tabs>
  <Tab title="Option A">
    Content for option A.
  </Tab>
  <Tab title="Option B">
    Content for option B.
  </Tab>
</Tabs>
```

### FAQ / Accordions

```mdx
<AccordionGroup>
  <Accordion title="Question text?">
    Answer content here.
  </Accordion>
</AccordionGroup>
```

---

## Code Examples

Always include a `title` attribute describing what the block shows:

````mdx
```yaml title="Example pipeline configuration"
tasks:
  - task_id: my_task
    type: BASH
```
````

Use `yaml`, `python`, `sql`, `bash`, `json` as the language identifier as appropriate.

---

## Tables

Use for parameter/configuration reference. Standard columns for integration docs:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `task_id` | string | Yes | Unique identifier for the task |

---

## Integration Doc Pattern

Integration pages (under `/docs/integrations/`) follow this structure:

1. Intro — what this integration does
2. **Prerequisites** — what must be set up first
3. **Authentication** — step-by-step with `<Frame>` screenshots
4. **Parameters** — table of all config fields
5. **Example** — YAML configuration block

---

## Navigation (`docs.json`)

When adding a new page:

1. Create the `.mdx` file in the appropriate `/docs/` subdirectory.
2. Add the path (without extension) to the correct `group` in `docs.json`:

```json
{
  "group": "My Section",
  "pages": [
    "docs/my-section/existing-page",
    "docs/my-section/new-page"
  ]
}
```

For a new section with child pages, add a nested group:

```json
{
  "group": "New Feature",
  "expanded": false,
  "pages": [
    "docs/new-feature/",
    "docs/new-feature/sub-page-one",
    "docs/new-feature/sub-page-two"
  ]
}
```

The first entry (`"docs/new-feature/"`) should be the `index.mdx` overview page.

If adding a new top-level section with a root prefix:

```json
{
  "group": "New Section",
  "root": "docs/new-section/",
  "pages": [...]
}
```

---

## Linting & Validation

Before committing:

- `yarn lint` — runs markdownlint, image validation, and filetype checks
- `vale docs/` — spelling and style checks (custom vocabulary in `.vale/styles/config/vocabularies/`)

Rules in force (see `.markdownlint.json`):

- Long lines are allowed
- Inline HTML/JSX is allowed
- First line does not need to be `# H1` (frontmatter title is used)

---

## What Not To Do

- Don't use `.md` file extensions — must be `.mdx`
- Don't write `[click here](url)` or `[here](url)` link text
- Don't add images without `<Frame>`, captions, and alt text
- Don't skip frontmatter `title` or `description`
- Don't add a page without registering it in `docs.json`
- Don't invent component syntax — only use documented Mintlify components
- Don't use passive voice or filler phrases like "In order to..." or "It should be noted that..."
