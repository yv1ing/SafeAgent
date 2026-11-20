# Console

[中文](console_zh.md)

The console is the operator's view of the gateway. It shows incoming traffic, how each call was processed, and the policy decisions taken for it.

## Organization

Everything is organized by project. A project owns its model routes, redaction rules, policy, access keys and audit trail, so those settings live inside the project.

Outside a project, the sidebar offers Monitor and Projects, plus Channels, Users and Log for administrators. Inside a project it offers Overview, Calls, Conversations, Risk, Activity, Policies, Redaction, Agent keys, Model routes, Members and Settings. The top bar contains the project switcher, the breadcrumb, language and theme controls, and the account menu.

## Common behavior

Lists share one filter and paging pattern. The calls page keeps its filter in the address bar, so a filtered view can be reloaded or shared. On a wide screen a call opens beside the list; on a narrow screen it opens in a sheet.

Create and edit actions use dialogs, except project settings, channel settings and the policy editor, which are edited in place. The policy editor offers save and discard; project and channel settings save changes when the save button is used. Destructive actions ask for confirmation. Keys and passwords are shown once with a copy action.

The interface shows only the actions a user is allowed to take. Project members manage routes, non-blocking redaction rules, record-only policy rules and agent keys; trust levels and non-default roles are administrator-only. Administrators also manage channels, users, the global log, project creation and deletion, members, blocking policy and redaction rules, policy restore, the decision model, and the retention and limit fields in project settings.

Channels also carry a default output limit and a list of client headers the gateway may forward. Project settings provide a project-wide default. The limits matter when a call is sent to an Anthropic Messages upstream, which requires a value: an explicit client limit wins, then a route-target override (set through the API), the channel default, the project default, and finally the gateway fallback. Other protocols leave an omitted limit to the provider.

## Calls

A call page shows metrics, online decisions, the conversation, upstream attempts and asynchronous analyses. Raw events are visible to administrators only.

Online decisions show which rule or model answer allowed or blocked a request or tool call. The online path stores decisions; the findings and relations from diagnosis appear under the analyses.

Each analysis shows its verdict, coverage, detector and taxonomy versions, the decision model's answer when one was made, its findings and its relation edges. A finding shows its code, origin, sink, the three axes, severity, confidence, detector and evidence. A failed analysis keeps its error and coverage state. The page can trigger re-analysis; the new result is stored beside the old one and can be compared when the input, detector and catalogue versions match.

## Risk

The project Risk page shows distributions over risk sources, failure modes, harms and severity, the most frequent finding codes, and the agents with the most findings or flows. Selecting an agent lists the relation edges for the selected time window: which external content reached which tool call, by value reuse or by following an instruction.

Flow edges describe relations inside one call. They do not claim that an earlier call caused a later one; the page aggregates them by agent key for display.

## Policy and redaction

The Policies page edits one ordered list of CEL rules. The editor validates before saving, and saving activates the new version in one transaction. The history lists every saved version, marks the active one, and offers restore for administrators. Restoring an older document creates a new version; the history is never rewritten.

Members may edit record rules without priorities. Blocking rules written by an administrator must remain in the document unchanged; members cannot add, modify or remove them. Priorities, restore and the decision model are administrator-only.

The Redaction page manages built-in detectors and custom patterns, and lets an operator test a pattern against sample text. A blocking redaction rule rejects a request before it reaches the provider; only administrators can add, change or remove one.

## Decision model

Project settings, not the policy document, configure the decision model. It is optional. When enabled, it labels every analyzed trajectory on the full taxonomy and answers an authorization question before a side-effecting tool call is released. The API key is write-only; the interface shows only whether one is stored.

## Visual language

The interface uses a near-white background and a single indigo accent for structure; color is reserved for state and charts. Light and dark themes share the same palette and focus treatment, so keyboard focus remains clearly visible in both.

The layout is compact, with small type, tabular numbers and thin borders; cards appear only where a group of content needs a boundary. On small screens the sidebar becomes a drawer and wide tables scroll horizontally.

## Current design trade-offs

- **Hidden reasoning** — The console can show only what the provider returns.
- **No execution proof** — A recorded tool call and result do not prove that the side effect occurred.
- **Text-only redaction** — Images and audio are outside the redaction engine.
- **Findings need confirmation** — Relations and capabilities are heuristic; an unfinished analysis is reported as `unknown`.
- **No online response block** — Response prose is reviewed after the call, once it has reached the client.

## Related

- [Architecture](architecture.md)
- [Getting started](getting-started.md)
- [Agent threat modeling](threat-modeling.md)
