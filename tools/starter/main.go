package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fatal(fmt.Errorf("usage: starter remove|check|upgrade"))
	}
	var err error
	switch os.Args[1] {
	case "remove":
		err = runRemove(os.Args[2:])
	case "check":
		err = runCheck(os.Args[2:])
	case "upgrade":
		err = runUpgrade(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func runRemove(args []string) error {
	flags := flag.NewFlagSet("remove", flag.ContinueOnError)
	root := flags.String("root", ".", "repository root")
	removed := flags.String("removed", "", "comma-separated modules")
	target := flags.String("target", "", "replay target version")
	noRecord := flags.Bool("no-record", false, "do not update starter-removed.yaml")
	dryRun := flags.Bool("dry-run", false, "only print deletion set")
	if err := flags.Parse(args); err != nil {
		return err
	}
	modules := splitModules(*removed)
	if *target != "" {
		previous, err := removedModules(*root)
		if err != nil {
			return err
		}
		modules = previous
	}
	if len(modules) == 0 {
		return fmt.Errorf("remove: --removed is required")
	}
	if *target == "" && !*dryRun {
		cmd := exec.Command("git", "-C", *root, "status", "--porcelain")
		out, err := cmd.Output()
		if err != nil {
			return err
		}
		if len(out) > 0 {
			return fmt.Errorf("remove: workspace must be clean")
		}
	}
	plan, err := compute(repository{root: *root, target: *target}, modules)
	if err != nil {
		return err
	}
	if err := apply(*root, plan, applyOptions{NoRecord: *noRecord || *target != "", DryRun: *dryRun}); err != nil {
		return err
	}
	if *dryRun {
		return nil
	}
	return generateAndTest(*root, plan.AdminSurface)
}

func runCheck(args []string) error {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil {
		return err
	}
	modules := flags.Args()
	if len(modules) == 0 {
		var err error
		modules, err = allModules(*root)
		if err != nil {
			return err
		}
	}
	return check(*root, modules)
}

func splitModules(value string) []string {
	var out []string
	for module := range strings.SplitSeq(value, ",") {
		if module = strings.TrimSpace(module); module != "" {
			out = append(out, module)
		}
	}
	return out
}
