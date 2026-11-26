package app

import (
	"strings"
	"unicode"
)

// scaffoldFiles returns the files of a new scenario folder, keyed by path
// inside the folder. Every field of the Scenario Standard is present with a
// TODO to replace, and the file passes validation as it is.
func scaffoldFiles(id, product string) map[string]string {
	r := strings.NewReplacer(
		"{{id}}", id,
		"{{name}}", titleFromID(id),
		"{{product}}", product,
		"{{productTitle}}", strings.ToUpper(product[:1])+product[1:],
	)
	return map[string]string{
		"scenario.yaml":      r.Replace(scaffoldScenario),
		"steps/01-break.md":  r.Replace(scaffoldBreak),
		"steps/02-fix.md":    r.Replace(scaffoldFix),
		"templates/.gitkeep": "",
		"fixtures/.gitkeep":  "",
	}
}

// titleFromID turns vault-raft-lost-quorum into "Vault Raft Lost Quorum".
func titleFromID(id string) string {
	words := strings.Split(id, "-")
	for i, w := range words {
		if w != "" {
			rs := []rune(w)
			rs[0] = unicode.ToUpper(rs[0])
			words[i] = string(rs)
		}
	}
	return strings.Join(words, " ")
}

const scaffoldScenario = `id: {{id}}
name: {{name}}
summary: TODO One sentence for the catalogue.
product: {{product}}
editions: [community]
difficulty: beginner
estimatedMinutes: 15
tags: [{{product}}]
objectives:
  - TODO What the trainee should be able to do afterwards.
prerequisites:
  - TODO Knowledge and tools needed.
parameters:
  - id: version
    type: version
    product: {{product}}
    label: {{productTitle}} version
topology:
  clusters:
    - name: primary
      product: {{product}}
      nodes: 1
initialState: TODO What the deployment provides before step one.
failureCreated: TODO What is broken, the symptoms, and whether it is recoverable.
successCriteria: TODO What "fixed" means.
pitfalls:
  - TODO A common mistake and the symptom it causes.
references:
  - title: TODO Product documentation
    url: https://developer.hashicorp.com/{{product}}
steps:
  - id: break
    kind: breakage
    title: TODO Apply the breakage
    guide: steps/01-break.md
    action:
      - primitive: container.exec
        args: { node: node-1, command: [sh, -c, "echo TODO break something"] }
    verify:
      - primitive: container.exec
        args: { node: node-1, command: [sh, -c, "echo TODO check that it is broken"] }
  - id: fix
    kind: verification
    title: TODO Check the fix
    guide: steps/02-fix.md
    resetPoint: true
    verify:
      - primitive: container.exec
        args: { node: node-1, command: [sh, -c, "echo TODO check that it is fixed"] }
`

const scaffoldBreak = `# TODO Apply the breakage

## Briefing

TODO What the instructor tells the trainee: the symptom, not the cause.

## Hints

TODO Nudges the instructor can give, from gentle to direct.

## Solution

TODO What fixes it, and why.
`

const scaffoldFix = `# TODO Check the fix

## Briefing

TODO What the trainee has been asked to achieve.

## Hints

TODO Nudges the instructor can give.

## Solution

TODO The commands that fix it.
`
