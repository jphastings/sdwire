---
# SDW-02n2
title: 'CI: Windows gofmt failure from CRLF checkouts'
status: completed
type: bug
priority: normal
created_at: 2026-08-08T20:00:37Z
updated_at: 2026-08-08T20:00:48Z
---

The windows-latest job checks out with core.autocrlf, converting every file to CRLF; gofmt -l then lists the entire tree as unformatted. Fix: .gitattributes forcing eol=lf for Go files (+ text=auto), and scope the OS-independent gofmt check to the Linux job.

- [x] .gitattributes with *.go/go.mod/go.sum eol=lf; git add --renormalize confirmed a no-op (tree already LF)
- [x] gofmt check runs only on Linux (formatting is OS-independent)

## Summary of Changes

.gitattributes pins LF for Go files so Windows checkouts stop producing CRLF working trees; the CI gofmt check is scoped to the Linux job since formatting is OS-independent (and the other jobs' build/vet/test steps are unaffected).
