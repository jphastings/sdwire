---
# SDW-c0iq
title: 'CI: bump actions to Node 24 versions (checkout v7, setup-go v7)'
status: completed
type: task
created_at: 2026-08-08T20:07:22Z
updated_at: 2026-08-08T20:07:22Z
---

GitHub warns that checkout@v4/setup-go@v5 target deprecated Node 20. Bumped to checkout v7.0.1 / setup-go v7.0.0, pinned by commit SHA (verified against the official tags via the GitHub API) with version comments.

- [x] SHAs verified to match actions/checkout v7.0.1 and actions/setup-go v7.0.0 tags
- [x] Warnings confirmed gone on the next CI run
