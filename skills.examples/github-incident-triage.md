---
name: github-incident-triage
description: Multi-action workflow for GitHub incident triage with retries, branching, loops, auth, and shaped output
runtimeMinVersion: 0.1.0

httpAuth:
  gh:
    type: bearer
    token: "{{secret:GITHUB_TOKEN}}"

tools:
  - name: incident_ops
    description: Triage GitHub issues for a repo
    type: workflow
    actionField: action
    parameters:
      type: object
      properties:
        action:
          type: string
          enum: ["scan_open_issues", "summarize_issue", "label_stale"]
        owner:
          type: string
        repo:
          type: string
        issueNumber:
          type: integer
        staleDays:
          type: integer
      required: ["action", "owner", "repo"]

    actions:
      scan_open_issues:
        - name: fetch_open
          type: http
          method: GET
          url: "https://api.github.com/repos/{{owner}}/{{repo}}/issues?state=open&per_page=30"
          auth: gh
          headers:
            Accept: application/vnd.github+json
          result: json
          retries: 2
          retryDelayMs: 400
          extract: "."
          outputSchema:
            type: array

        - name: filter_non_prs
          type: forEach
          forEach: "steps.fetch_open"
          as: issue
          steps:
            - name: keep_issue_only
              type: switch
              switch: "issue.pull_request"
              cases:
                - match: null
                  steps:
                    - name: emit_issue
                      type: shell
                      command: ["echo", "{{issue.number}}|{{issue.title|default:untitled}}|{{issue.updated_at}}"]
              default: []

        - name: summarize
          type: shell
          command:
            [
              "echo",
              "Open issues scanned for {{owner}}/{{repo}}. Count={{steps.fetch_open|json}}"
            ]
          onError: continue

      summarize_issue:
        - name: fetch_issue
          type: http
          method: GET
          url: "https://api.github.com/repos/{{owner}}/{{repo}}/issues/{{issueNumber}}"
          auth: gh
          headers:
            Accept: application/vnd.github+json
          result: json
          select:
            number: "number"
            title: "title"
            state: "state"
            labels: "labels"
            updated: "updated_at"
          saveAs: issue
          outputSchema:
            type: object
            required: ["number", "title", "state"]

        - name: build_summary
          type: shell
          command:
            [
              "echo",
              "Issue #{{steps.issue.number}} {{steps.issue.title}} ({{steps.issue.state}}), updated={{steps.issue.updated}}"
            ]

      label_stale:
        - name: list_open
          type: http
          method: GET
          url: "https://api.github.com/repos/{{owner}}/{{repo}}/issues?state=open&per_page=50"
          auth: gh
          headers:
            Accept: application/vnd.github+json
          result: json
          retries: 2

        - name: each_issue
          type: forEach
          forEach: "steps.list_open"
          as: issue
          steps:
            - name: maybe_label
              if: "issue.pull_request == null"
              type: http
              method: POST
              url: "https://api.github.com/repos/{{owner}}/{{repo}}/issues/{{issue.number}}/labels"
              auth: gh
              headers:
                Accept: application/vnd.github+json
                Content-Type: application/json
              body: "{\"labels\":[\"stale\"]}"
              onError: continue

    finally:
      - name: audit_log
        type: shell
        command: ["echo", "incident_ops action={{action}} repo={{owner}}/{{repo}} done"]
        onError: continue
---
Use `incident_ops` with one action at a time.
Requires `secrets.GITHUB_TOKEN` (or `GITHUB_TOKEN` env var).
