# Consensus Warnings GitHub Action

The consensuswarn GitHub action takes a set of Go function and method roots and checks whether the
current PR touches any of the roots, or any function or method directly or indirectly called by
a root. If so, a comment is posted on the PR with the affected callstacks.

Function and methods roots are specified on the form

```
example.com/pkg/path.Type.Method
```

functions use

```
example.com/pkg/path.Function
```

## Example Workflow

```
name: "Warn about consensus code changes"

on:
  pull_request_target:
    types:
      - opened
      - edited
      - synchronize

jobs:
  main:
    permissions:
      pull-requests: write # For reading the PR and posting comment
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: orijtech/consensuswarn@main
        with:
          roots: 'github.com/cosmos/cosmos-sdk/baseapp.BaseApp.DeliverTx,github.com/cosmos/cosmos-sdk/baseapp.BaseApp.BeginBlock,github.com/cosmos/cosmos-sdk/baseapp.BaseApp.EndBlock,github.com/cosmos/cosmos-sdk/baseapp.BaseApp.Commit'
```
