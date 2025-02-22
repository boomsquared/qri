package cmd

import (
	"bytes"
	"fmt"
	"regexp"

	util "github.com/qri-io/apiutil"
	"github.com/qri-io/dataset"
	"github.com/qri-io/ioes"
	"github.com/qri-io/qri/lib"
	"github.com/spf13/cobra"
)

// NewGetCommand creates a new `qri search` command that searches for datasets
func NewGetCommand(f Factory, ioStreams ioes.IOStreams) *cobra.Command {
	o := &GetOptions{IOStreams: ioStreams}
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get elements of qri datasets",
		Long: `Get the qri dataset (except for the body). You can also get portions of 
the dataset: meta, structure, viz, transform, and commit. To narrow down
further to specific fields in each section, use dot notation. The get 
command prints to the console in yaml format, by default.

You can get pertinent information on multiple datasets at the same time
by supplying more than one dataset reference.

Check out https://qri.io/docs/reference/dataset/ to learn about each section of the 
dataset and its fields.`,
		Example: `  # print the entire dataset to the console
  qri get me/annual_pop

  # print the meta to the console
  qri get meta me/annual_pop

  # print the dataset body size to the console
  qri get structure.length me/annual_pop

  # print the dataset body size for two different datasets
  qri get structure.length me/annual_pop me/annual_gdp`,
		Annotations: map[string]string{
			"group": "dataset",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Special case for --pretty, check if it was passed vs if the default was used.
			if cmd.Flags().Changed("pretty") {
				o.HasPretty = true
			}
			if err := o.Complete(f, args); err != nil {
				return err
			}
			return o.Run()
		},
	}

	cmd.Flags().StringVarP(&o.Format, "format", "f", "", "set output format [json, yaml]")
	cmd.Flags().BoolVar(&o.Pretty, "pretty", false, "whether to print output with indentation, only for json format")
	cmd.Flags().IntVar(&o.PageSize, "page-size", -1, "for body, limit how many entries to get per page")
	cmd.Flags().IntVar(&o.Page, "page", -1, "for body, page at which to get entries")
	cmd.Flags().BoolVarP(&o.All, "all", "a", true, "for body, whether to get all entries")

	return cmd
}

// GetOptions encapsulates state for the get command
type GetOptions struct {
	ioes.IOStreams

	Refs     *RefSelect
	Selector string
	Format   string

	Page     int
	PageSize int
	All      bool

	Pretty    bool
	HasPretty bool

	DatasetRequests *lib.DatasetRequests
}

// isDatasetField checks if a string is a dataset field or not
var isDatasetField = regexp.MustCompile("(?i)^(commit|cm|structure|st|body|bd|meta|md|viz|vz|transform|tf|rendered|rd)($|\\.)")

// Complete adds any missing configuration that can only be added just before calling Run
func (o *GetOptions) Complete(f Factory, args []string) (err error) {
	if o.DatasetRequests, err = f.DatasetRequests(); err != nil {
		return
	}

	if len(args) > 0 {
		if isDatasetField.MatchString(args[0]) {
			o.Selector = args[0]
			args = args[1:]
		}
	}
	if o.Refs, err = GetCurrentRefSelect(f, args, -1); err != nil {
		return
	}

	if o.Selector == "body" {
		// if we have a PageSize, but not Page, assume an Page of 1
		if o.PageSize != -1 && o.Page == -1 {
			o.Page = 1
		}
		// set all to false if PageSize or Page values are provided
		if o.PageSize != -1 || o.Page != -1 {
			o.All = false
		}
	} else {
		if o.PageSize != -1 {
			return fmt.Errorf("can only use --page-size flag when getting body")
		}
		if o.Page != -1 {
			return fmt.Errorf("can only use --page flag when getting body")
		}
		if !o.All {
			return fmt.Errorf("can only use --all flag when getting body")
		}
	}

	return nil
}

// Run executes the get command
func (o *GetOptions) Run() (err error) {
	printRefSelect(o.Out, o.Refs)

	// Pretty maps to a key in the FormatConfig map.
	var fc dataset.FormatConfig
	if o.HasPretty {
		opt := dataset.JSONOptions{Options: make(map[string]interface{})}
		opt.Options["pretty"] = o.Pretty
		fc = &opt
	}

	// convert Page and PageSize to Limit and Offset
	page := util.NewPage(o.Page, o.PageSize)
	// TODO(dlong): Restore ability to `get` from multiple datasets at once.
	p := lib.GetParams{
		Path:         o.Refs.Ref(),
		Selector:     o.Selector,
		UseFSI:       o.Refs.IsLinked(),
		Format:       o.Format,
		FormatConfig: fc,
		Offset:       page.Offset(),
		Limit:        page.Limit(),
		All:          o.All,
	}
	res := lib.GetResult{}
	if err = o.DatasetRequests.Get(&p, &res); err != nil {
		return err
	}

	buf := bytes.NewBuffer(res.Bytes)
	buf.Write([]byte{'\n'})
	printToPager(o.Out, buf)
	return
}
