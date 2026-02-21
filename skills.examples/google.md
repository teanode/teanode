---
name: google-extras
description: Additional Google Workspace tools via gogcli for services not covered by built-in tools (Keep, Docs, Sheets, etc.)
tools:
  - name: google_keep_list
    description: List Google Keep notes
    type: shell
    command: ["gog", "--json", "--no-input", "--results-only", "keep", "list"]
    timeout: 30
    parameters:
      type: object
      properties: {}

  - name: google_keep_search
    description: Search Google Keep notes
    type: shell
    command: ["gog", "--json", "--no-input", "--results-only", "keep", "search", "--query", "{{query}}"]
    timeout: 30
    parameters:
      type: object
      properties:
        query:
          type: string
          description: Search query for Keep notes
      required: ["query"]

  - name: google_keep_create
    description: Create a new Google Keep note
    type: shell
    command: ["gog", "--json", "--no-input", "--results-only", "keep", "create", "--title", "{{title}}", "--body", "{{body}}"]
    timeout: 30
    parameters:
      type: object
      properties:
        title:
          type: string
          description: Note title
        body:
          type: string
          description: Note body text
      required: ["title", "body"]
---
