# Example module revisions

Four revisions of the same fictional VPC module. They are the fixtures the test
suite runs against, and they double as a way to try the tool without a repo:

| from | to | severity | why |
| --- | --- | --- | --- |
| `vpc-v1.0.0` | `vpc-v1.0.0` | patch | nothing changed |
| `vpc-v1.0.0` | `vpc-v1.1.0` | minor | a new optional input, a new output, a new optional object attribute |
| `vpc-v1.1.0` | `vpc-v1.1.1` | patch | a reworded description and a changed default value |
| `vpc-v1.1.0` | `vpc-v2.0.0` | major | a removed input, a dropped default, `any` narrowed to an object, an output turned sensitive |

Try them:

```console
$ tf-module-api-diff --from-dir examples/vpc-v1.0.0 --to-dir examples/vpc-v1.1.0
examples/vpc-v1.0.0 -> examples/vpc-v1.1.0
severity: minor
...
```

```console
$ tf-module-api-diff --from-dir examples/vpc-v1.1.0 --to-dir examples/vpc-v2.0.0 --expect patch; echo "exit $?"
...
exit 1
```
