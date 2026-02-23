# Agent Instructions

- On every startup for this repository, read `README.md` before making changes.
- If npm is not available, check if you need to source rvm.
- Make sure you keep code always formatted.

### Naming Convention

When first alphabetical character is capitalized, also capitalize acronyms:

- `ReferenceURI`
- `URL`
- `ID`
- `SessionID`
- `GetFTPID`
- `_CreateSessionID`

When first alphabetical character is not capitalized, capitalize **first** letter of an acronym:

- `referenceUri`
- `url`
- `id`
- `sessionId`
- `getFtpId`
- `__deleted__`
- `__somethingElse__`

Do not abbreviate, spell things out clearly. For example:

- prefer "command" over "cmd"
- prefer "response" over "resp"
- prefer "request" over "req"

Package names being the exception, they should be brief.

Avoid single letter variables.

Name things consistently everywhere, do not give different name to the same thing.

When writing member function of a struct in Golang, use `self` to refer to the instance.

