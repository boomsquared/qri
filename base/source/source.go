package source

import (
	"fmt"

	"github.com/qri-io/dataset"
	"github.com/qri-io/dataset/dsio"
	"github.com/qri-io/dataset/vals"
)

// // Resolver
// type Resolver struct {
// 	fs qfs.Filesystem
// }

// // Resolve returns a value for a path
// func (s *Source) Resolve(path string) (value interface{}, err error) {
// 	return nil, fmt.Errorf("not finished")
// }

// Dataset implements dataset as a traversable linked object
type Dataset dataset.Dataset

// ValueForKey returns the given
func (ds *Dataset) ValueForKey(key string) (val interface{}, err error) {
	switch key {
	case "body":
		d := &dataset.Dataset{}
		*d = dataset.Dataset(*ds)
		if d.BodyFile() != nil && d.Structure != nil {
			er, err := dsio.NewEntryReader(d.Structure, d.BodyFile())
			if err != nil {
				return nil, err
			}

			return &DsioIterator{rdr: er}, nil
		}
		return ds.Body, nil
	case "schema":
		if ds.Structure != nil {
			return ds.Structure.Schema, nil
		}
		return nil, nil
	case "bodyBytes":
		return ds.BodyBytes, nil
	case "bodyPath":
		return ds.BodyPath, nil
	case "commit":
		return ds.Commit, nil
	case "meta":
		return ds.Meta, nil
	case "name":
		return ds.Name, nil
	case "path":
		return ds.Path, nil
	case "peername":
		return ds.Peername, nil
	case "previousPath":
		return ds.PreviousPath, nil
	case "profileID":
		return ds.ProfileID, nil
	case "numVersions":
		return ds.NumVersions, nil
	case "qri":
		return ds.Qri, nil
	case "structure":
		return ds.Structure, nil
	case "transform":
		return ds.Transform, nil
	case "viz":
		return ds.Viz, nil
	default:
		return nil, fmt.Errorf("key not found: %s", key)
	}
}

// DsioIterator is an iterator created from a dsio.EntryReader
type DsioIterator struct {
	i   int
	rdr dsio.EntryReader
}

var _ vals.Iterator = (*DsioIterator)(nil)

// Next returns the next value in an entry
func (it *DsioIterator) Next() (*vals.Entry, bool) {
	it.i++
	ent, err := it.rdr.ReadEntry()
	if err != nil {
		if err.Error() == "EOF" {
			return nil, true
		}
		panic(err)
	}
	return &vals.Entry{
		Index: it.i - 1,
		Key:   ent.Key,
		Value: ent.Value,
	}, false
}

// Done closes the reader
func (it *DsioIterator) Done() {
	if err := it.rdr.Close(); err != nil {
		panic(err)
	}
}

// ValueForIndex returns the value at a given index
func (it *DsioIterator) ValueForIndex(i int) (v interface{}, err error) {
	defer it.rdr.Close()

	for {
		ent, err := it.rdr.ReadEntry()
		if err != nil {
			return nil, err
		}
		if it.i == i {
			return ent.Value, nil
		}
		it.i++
	}
}
