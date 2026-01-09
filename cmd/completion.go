package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/posener/complete"
)

// completionCmd prints the shell snippet needed to activate tab-completion.
type completionCmd struct {
	Shell string `arg:"" optional:"" help:"Shell name (bash, zsh, fish). Auto-detected if omitted."`
	Code  bool   `short:"c" help:"Print raw init code (for eval / source)."`
}

func (c *completionCmd) Help() string {
	return `Prints the command you need to run to activate tab-completion.

For permanent activation, paste the command into your shell's init
file (~/.bashrc, ~/.zshrc, or ~/.config/fish/config.fish).`
}

func (c *completionCmd) Run(ctx *kong.Context) error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine binary path: %w", err)
	}
	bin, _ = filepath.Abs(bin)
	name := ctx.Model.Name

	shell := c.Shell
	if shell == "" {
		shell = detectShell()
		if shell == "" {
			return fmt.Errorf("could not detect shell; pass it explicitly: cartograph completion bash")
		}
	}

	initCode, ok := shellInitCode[shell]
	if !ok {
		return fmt.Errorf("unsupported shell: %s", shell)
	}

	code := strings.NewReplacer("{{BIN}}", bin, "{{NAME}}", name).Replace(initCode)

	if c.Code {
		fmt.Fprintln(ctx.Stdout, code)
	} else {
		fmt.Fprintf(ctx.Stdout,
			"Run this to activate tab-completion for %s in %s:\n\n    source <(%s completion -c %s)\n\nFor permanent use, add it to %s\n",
			name, shell, name, shell, shellInitFile[shell],
		)
	}
	ctx.Exit(0)
	return nil
}

var shellInitCode = map[string]string{
	"bash": "complete -o default -o bashdefault -C {{BIN}} {{NAME}}",
	"zsh":  "autoload -U +X bashcompinit && bashcompinit\ncomplete -o default -o bashdefault -C {{BIN}} {{NAME}}",
	"fish": `function __complete_{{NAME}}
    set -lx COMP_LINE (commandline -cp)
    test -z (commandline -ct)
    and set COMP_LINE "$COMP_LINE "
    {{BIN}}
end
complete -f -c {{NAME}} -a "(__complete_{{NAME}})"`,
}

var shellInitFile = map[string]string{
	"bash": "~/.bashrc",
	"zsh":  "~/.zshrc",
	"fish": "~/.config/fish/config.fish",
}

// detectShell returns "bash", "zsh", or "fish" based on $SHELL.
func detectShell() string {
	sh := filepath.Base(os.Getenv("SHELL"))
	if _, ok := shellInitCode[sh]; ok {
		return sh
	}
	return ""
}

const predictorTag = "completion-predictor"

// RegisterCompletion intercepts completion requests (via COMP_LINE)
// before kong parses normally. If the shell is requesting completions,
// it prints them and exits.
func RegisterCompletion(parser *kong.Kong, predictors map[string]complete.Predictor) {
	cmd, err := buildCommand(parser.Model.Node, predictors)
	if err != nil {
		return
	}
	cmp := complete.New(parser.Model.Name, *cmd)
	cmp.Out = parser.Stdout
	if cmp.Complete() {
		parser.Exit(0)
	}
}

// buildCommand recursively converts a kong node tree into
// posener/complete command descriptors.
func buildCommand(node *kong.Node, predictors map[string]complete.Predictor) (*complete.Command, error) {
	if node == nil {
		return nil, nil
	}

	cmd := complete.Command{
		Sub:         complete.Commands{},
		GlobalFlags: complete.Flags{},
	}

	for _, child := range node.Children {
		if child == nil || child.Hidden {
			continue
		}
		sub, err := buildCommand(child, predictors)
		if err != nil {
			return nil, err
		}
		if sub != nil {
			cmd.Sub[child.Name] = *sub
		}
	}

	boolFlags, argFlags := splitFlags(node.Flags)
	for _, f := range node.Flags {
		if f == nil || f.Hidden {
			continue
		}
		p := predictorForValue(f.Value, predictors)
		for _, name := range flagNames(f) {
			cmd.GlobalFlags[name] = p
		}
	}

	positionals := make([]complete.Predictor, len(node.Positional))
	for i, arg := range node.Positional {
		positionals[i] = predictorForValue(arg, predictors)
	}
	isCumulative := len(node.Positional) > 0 && node.Positional[len(node.Positional)-1].IsCumulative()

	cmd.Args = &positionalPredictor{
		predictors:   positionals,
		argFlags:     flagNames(argFlags...),
		boolFlags:    flagNames(boolFlags...),
		isCumulative: isCumulative,
	}

	return &cmd, nil
}

// predictorForValue returns a predictor for a kong value, checking the
// completion-predictor tag, then enum values, then falling back to
// PredictAnything.
func predictorForValue(v *kong.Value, predictors map[string]complete.Predictor) complete.Predictor {
	if v == nil {
		return nil
	}
	if v.Tag != nil && v.Tag.Has(predictorTag) {
		name := v.Tag.Get(predictorTag)
		if p, ok := predictors[name]; ok {
			return p
		}
	}
	if v.IsBool() {
		return complete.PredictNothing
	}
	if v.Enum != "" {
		vals := make([]string, 0, len(v.EnumMap()))
		for k := range v.EnumMap() {
			vals = append(vals, k)
		}
		return complete.PredictSet(vals...)
	}
	return complete.PredictAnything
}

// splitFlags partitions flags into bool and non-bool groups.
func splitFlags(flags []*kong.Flag) (boolFlags, argFlags []*kong.Flag) {
	for _, f := range flags {
		if f.Value.IsBool() {
			boolFlags = append(boolFlags, f)
		} else {
			argFlags = append(argFlags, f)
		}
	}
	return
}

// flagNames returns --long and -short forms for each flag.
func flagNames(flags ...*kong.Flag) []string {
	var out []string
	for _, f := range flags {
		out = append(out, "--"+f.Name)
		if f.Short != 0 {
			out = append(out, "-"+string(f.Short))
		}
	}
	return out
}

// positionalPredictor dispatches to the correct per-position predictor,
// accounting for consumed flags in COMP_LINE.
type positionalPredictor struct {
	predictors   []complete.Predictor
	argFlags     []string // flags that consume the next token
	boolFlags    []string // flags that don't consume a token
	isCumulative bool     // last positional accepts multiple values
}

func (p *positionalPredictor) Predict(a complete.Args) []string {
	idx := p.positionalIndex(a)
	if idx < len(p.predictors) {
		if p.predictors[idx] != nil {
			return p.predictors[idx].Predict(a)
		}
		return nil
	}
	if p.isCumulative && len(p.predictors) > 0 {
		last := p.predictors[len(p.predictors)-1]
		if last != nil {
			return last.Predict(a)
		}
	}
	return nil
}

// positionalIndex counts how many positional args have been completed,
// skipping tokens that are flags or flag arguments.
func (p *positionalPredictor) positionalIndex(a complete.Args) int {
	idx := 0
	for i := 0; i < len(a.Completed); i++ {
		tok := a.Completed[i]
		if p.isFlag(tok) {
			if !p.isBoolFlag(tok) && !strings.Contains(tok, "=") {
				i++ // skip the flag's value token
			}
			continue
		}
		// Check if previous token was a non-bool flag (making this a flag value).
		if i > 0 && p.isArgFlag(a.Completed[i-1]) && !strings.Contains(a.Completed[i-1], "=") {
			continue
		}
		idx++
	}
	return idx
}

func (p *positionalPredictor) isFlag(s string) bool {
	s = strings.SplitN(s, "=", 2)[0]
	return p.isBoolFlag(s) || p.isArgFlag(s)
}

func (p *positionalPredictor) isBoolFlag(s string) bool {
	return slices.Contains(p.boolFlags, s)
}

func (p *positionalPredictor) isArgFlag(s string) bool {
	return slices.Contains(p.argFlags, s)
}
