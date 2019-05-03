package base

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/qri-io/dataset"
	"github.com/qri-io/ioes"
	"github.com/qri-io/iso8601"
	"github.com/qri-io/qfs"
	"github.com/qri-io/qri/cron"
)

// DatasetSaveRunner returns a cron.RunFunc that invokes the "qri save" command
func DatasetSaveRunner(basepath string) cron.RunJobFunc {
	return func(ctx context.Context, streams ioes.IOStreams, job *cron.Job) error {
		args := []string{"save", job.Name}

		if o, ok := job.Options.(*cron.DatasetOptions); ok {
			if o.Title != "" {
				args = append(args, fmt.Sprintf(`--title="%s"`, o.Title))
			}
			if o.Message != "" {
				args = append(args, fmt.Sprintf(`--message="%s"`, o.Message))
			}
			if o.Recall != "" {
				args = append(args, fmt.Sprintf(`--recall="%s"`, o.Recall))
			}
			if o.BodyPath != "" {
				args = append(args, fmt.Sprintf(`--body="%s"`, o.BodyPath))
			}
			if len(o.FilePaths) > 0 {
				for _, path := range o.FilePaths {
					args = append(args, fmt.Sprintf(`--file="%s"`, path))
				}
			}

			// TODO (b5) - config and secrets

			boolFlags := map[string]bool{
				"--publish":     o.Publish,
				"--strict":      o.Strict,
				"--force":       o.Force,
				"--keep-format": o.ConvertFormatToPrev,
				"--no-render":   !o.ShouldRender,
			}
			for flag, use := range boolFlags {
				if use {
					args = append(args, flag)
				}
			}

		}

		cmd := exec.Command("qri", args...)
		// cmd.Dir = basepath
		cmd.Stderr = streams.ErrOut
		cmd.Stdout = streams.Out
		cmd.Stdin = streams.In
		return cmd.Run()
	}
}

// LocalShellScriptRunner creates a script runner anchored at a local path
// The runner it wires operating sytsem command in/out/errour to the iostreams
// provided by RunJobFunc. All paths are in relation to the provided base path
// Commands are executed with access to the same enviornment variables as the
// process the runner is executing in
// The executing command blocks until completion
func LocalShellScriptRunner(basepath string) cron.RunJobFunc {
	return func(ctx context.Context, streams ioes.IOStreams, job *cron.Job) error {
		path := job.Name
		if qfs.PathKind(job.Name) == "local" {
			// TODO (b5) - need to first check that path can't be found
			// path = filepath.Join(basepath, path)
		}

		cmd := exec.Command(path)
		// cmd.Dir = basepath
		cmd.Stderr = streams.ErrOut
		cmd.Stdout = streams.Out
		cmd.Stdin = streams.In
		return cmd.Run()
	}
}

// PossibleShellScript checks a path to see if it might be a shell script
// TODO (b5) - deal with platforms that don't use '.sh' as a script extension (windows?)
func PossibleShellScript(path string) bool {
	return filepath.Ext(path) == ".sh"
}

// DatasetToJob converts a dataset to cron.Job
func DatasetToJob(ds *dataset.Dataset, periodicity string, opts *cron.DatasetOptions) (job *cron.Job, err error) {
	if periodicity == "" && ds.Meta != nil && ds.Meta.AccrualPeriodicity != "" {
		periodicity = ds.Meta.AccrualPeriodicity
	}

	if periodicity == "" {
		return nil, fmt.Errorf("scheduling dataset updates requires a meta component with accrualPeriodicity set")
	}

	p, err := iso8601.ParseRepeatingInterval(periodicity)
	if err != nil {
		return nil, err
	}

	job = &cron.Job{
		// TODO (b5) - dataset.Dataset needs an Alias() method:
		Name:        fmt.Sprintf("%s/%s", ds.Peername, ds.Name),
		Periodicity: p,
		Type:        cron.JTDataset,
		LastRun:     ds.Commit.Timestamp,
	}
	if opts != nil {
		job.Options = opts
	}
	err = job.Validate()

	return
}

// ShellScriptToJob turns a shell script into cron.Job
func ShellScriptToJob(f qfs.File, periodicity string, opts *cron.ShellScriptOptions) (job *cron.Job, err error) {
	p, err := iso8601.ParseRepeatingInterval(periodicity)
	if err != nil {
		return nil, err
	}

	job = &cron.Job{
		Name:        f.FullPath(),
		Periodicity: p,
		Type:        cron.JTShellScript,
	}
	if opts != nil {
		job.Options = opts
	}
	err = job.Validate()
	return
}
