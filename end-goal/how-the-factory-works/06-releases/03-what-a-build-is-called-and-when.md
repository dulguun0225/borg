# What a build is called, and when

What the same build is called at each point, and what is versioned beside it.

| Name | What it is | When it applies |
|---|---|---|
| **candidate** | an item plus its build — identity enough to deploy, test, and reject | from build until merge to master |
| **release** | the name a build has on master | from merge to master onward |
| **contract version** | semver, one per published interface — a compatibility promise | moves when the form that interface publishes moves — an element added, removed, or redeclared, which of major or minor depending on whether the element is returned or accepted ([_Enforcement_](../07-contracts/04-enforcement.md)), and a [deprecation mark](../07-contracts/08-deprecation.md) on one |

A build [the search](../08-operations/03-overlapping-windows.md) calls for is not a third row: made by [the build runner](../05-environments/01-records-and-one-long-lived-branch.md) from commits master keeps, on no branch, it is never a candidate and never merges, so it takes neither name and is named only by the deploy record that carries it. A rejected candidate never needed a release number, and the number is not a third row here either: it is a field of the release, minted at the same event, and [_The release number_](04-the-release-number.md) is where it is set out. A build has one name at a time and the vocabulary stays at two, however many environments it runs on: a customer who defines five pre-production environments still has candidate and release, one build, and five deploy records — the names do not multiply with the environments.
