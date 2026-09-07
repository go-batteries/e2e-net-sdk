# Examples

## `lifecycle/`

End-to-end walkthrough of `myaccount/ec2` and `myaccount/iam`: lists your
projects/CRNs, looks up a registered SSH key, creates a node, polls until
it's running, logs its IP, SSH's in to prove it's reachable, then deletes it.

```
export E2E_API_KEY=...          # Settings -> API Keys in the E2E console
export E2E_PROJECT_ID=...       # Settings -> IAM
export E2E_SSH_KEY_LABEL=...    # a key already registered with E2E
export E2E_SSH_KEY_FILE=~/.ssh/e2e_network   # the matching private key

cd example/lifecycle
go run .
```

Set `E2E_KEEP=1` to skip the teardown step and leave the node running.

**Register the SSH key first, if you haven't:** MyAccount console -> Settings
-> SSH Keys -> Add SSH Key, paste the contents of the `.pub` file. The label
you give it there is `E2E_SSH_KEY_LABEL`.

This costs real money -- it creates a real node on your account (default plan
`C3.4GB`, override with `E2E_PLAN`) and deletes it when done, unless
`E2E_KEEP=1` is set.
