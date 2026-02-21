---
name: web-release-ops
description: Complex release workflow with multi-action routing, retries, branching, loops, auth, secrets, and output shaping
runtimeMinVersion: 0.1.0

httpAuth:
  github:
    type: bearer
    token: "{{secret:GITHUB_TOKEN}}"
  slack:
    type: bearer
    token: "{{secret:SLACK_BOT_TOKEN}}"

tools:
  - name: release_ops
    description: Run release actions for a GitHub repo
    type: workflow
    actionField: action
    parameters:
      type: object
      properties:
        action:
          type: string
          enum: ["collect", "notify", "close_milestone"]
        owner:
          type: string
        repo:
          type: string
        milestone:
          type: string
        slackChannel:
          type: string
      required: ["action", "owner", "repo"]

    actions:
      collect:
        - name: get_latest_release
          type: http
          method: GET
          url: "https://api.github.com/repos/{{owner}}/{{repo}}/releases/latest"
          auth: github
          headers:
            Accept: application/vnd.github+json
          result: json
          retries: 2
          retryDelayMs: 500
          select:
            tag: "tag_name"
            publishedAt: "published_at"
          saveAs: latest

        - name: list_open_issues
          type: http
          method: GET
          url: "https://api.github.com/repos/{{owner}}/{{repo}}/issues?state=open&per_page=20"
          auth: github
          headers:
            Accept: application/vnd.github+json
          result: json
          saveAs: openIssues

        - name: summarize_issues
          type: forEach
          forEach: "steps.openIssues"
          as: issue
          steps:
            - name: skip_prs
              type: switch
              switch: "issue.pull_request"
              cases:
                - match: null
                  steps:
                    - name: issue_line
                      type: shell
                      command: ["echo", "#{{issue.number}} {{issue.title|default:untitled}}"]
              default: []

      notify:
        - name: collect_if_needed
          type: switch
          switch: "steps.latest.tag"
          cases: []
          default:
            - name: fetch_latest
              type: http
              method: GET
              url: "https://api.github.com/repos/{{owner}}/{{repo}}/releases/latest"
              auth: github
              headers:
                Accept: application/vnd.github+json
              result: json
              select:
                tag: "tag_name"
                url: "html_url"
              saveAs: latest

        - name: post_slack
          type: http
          method: POST
          url: "https://slack.com/api/chat.postMessage"
          auth: slack
          headers:
            Content-Type: application/json
          body: "{\"channel\":\"{{slackChannel|default:#releases}}\",\"text\":\"New release {{owner}}/{{repo}} {{steps.latest.tag}} {{steps.latest.url}}\"}"
          result: json
          outputSchema:
            type: object
            required: ["ok"]
          onError: continue

      close_milestone:
        - name: list_milestones
          type: http
          method: GET
          url: "https://api.github.com/repos/{{owner}}/{{repo}}/milestones?state=open"
          auth: github
          headers:
            Accept: application/vnd.github+json
          result: json
          saveAs: milestones

        - name: find_target
          type: forEach
          forEach: "steps.milestones"
          as: ms
          steps:
            - name: match_milestone
              if: "ms.title == milestone"
              type: shell
              command: ["echo", "{{ms.number}}"]
              saveAs: targetMilestone

        - name: close_target
          type: http
          method: PATCH
          url: "https://api.github.com/repos/{{owner}}/{{repo}}/milestones/{{steps.targetMilestone}}"
          auth: github
          headers:
            Accept: application/vnd.github+json
            Content-Type: application/json
          body: "{\"state\":\"closed\"}"
          result: json
          onError: fail

    finally:
      - name: audit
        type: shell
        command: ["echo", "release_ops action={{action}} repo={{owner}}/{{repo}} done"]
        onError: continue
---
Use `release_ops` for one action at a time.
Configure `secrets.GITHUB_TOKEN` and `secrets.SLACK_BOT_TOKEN`.
