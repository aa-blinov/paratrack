# Agents

## After every push

Check the GitHub Actions run for that commit and wait until it finishes:

```bash
gh run list --limit 3
gh run watch <run-id> --exit-status
gh run view <run-id> --log-failed   # on failure
```

A red run is not done work: fix it and push again before reporting the task
finished. Tell the user the CI result (green, or what failed and the fix).
