package primitive

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	argPath = Arg{Name: "path", Type: String, Required: true, Help: "The absolute destination path inside the node."}
	argMode = Arg{Name: "mode", Type: String, Default: "0644", Help: "The file mode in octal, for example 0600."}
)

type fileRender struct{}

func (fileRender) Spec() Spec {
	return Spec{
		Name:    "file.render",
		Summary: "Renders a template from the scenario's templates folder with the run's values and writes it into a node. Rendering again overwrites the file, so it is safe to run again.",
		Timeout: 30 * time.Second,
		Changes: true,
		Args: []Arg{
			argNode,
			{Name: "template", Type: String, Required: true, Help: "The template's path inside the scenario folder, for example templates/vault.hcl.tmpl."},
			argPath, argMode,
		},
	}
}

func (fileRender) Run(ctx context.Context, env Env, a Args) (Result, error) {
	if env.ReadFile == nil || env.Render == nil {
		return Result{}, errors.New("this run has no scenario files or templates")
	}
	src, err := env.ReadFile(a.String("template"))
	if err != nil {
		return Result{}, fmt.Errorf("read template %s: %w", a.String("template"), err)
	}
	out, err := env.Render(string(src))
	if err != nil {
		return Result{}, fmt.Errorf("render %s: %w", a.String("template"), err)
	}
	return copyFile(ctx, env, a.String("node"), a.String("path"), a.String("mode"), []byte(out))
}

type fileWrite struct{}

func (fileWrite) Spec() Spec {
	return Spec{
		Name:    "file.write",
		Summary: "Writes text into a file in a node. Writing again overwrites the file, so it is safe to run again.",
		Timeout: 30 * time.Second,
		Changes: true,
		Args: []Arg{
			argNode, argPath, argMode,
			{Name: "content", Type: String, Required: true, Help: "The text to write."},
		},
	}
}

func (fileWrite) Run(ctx context.Context, env Env, a Args) (Result, error) {
	return copyFile(ctx, env, a.String("node"), a.String("path"), a.String("mode"), []byte(a.String("content")))
}
