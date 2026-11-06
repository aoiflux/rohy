package api

import (
	"errors"
	"fmt"

	"rohy/backend/consts"
	"rohy/backend/query"
)

// QueryAPI is the read-only query console's binding (plan §4.7, item 33). It adapts arguments to
// backend/query and holds no logic of its own.
//
// 🔒 It holds query.Reader, the read-only slice of the store, never *graphene.Store: a write is out
// of its reach at compile time, not only by convention. Its exported methods are exactly Describe
// and Run, and api_query_test.go holds that. No mutation binding is ever added here.
//
// Reads take no case lock (api_caselock.go), and neither does this: a query during an ingest or a
// build sees the case as it is, and its result says when the case moved under it.
type QueryAPI struct {
	reader query.Reader
}

// NewQueryAPI binds the console to reader, in production the case's *graphene.Store.
func NewQueryAPI(reader query.Reader) *QueryAPI {
	return &QueryAPI{reader: reader}
}

// Describe returns the console's vocabulary: subjects, fields, operators, columns and caps.
func (a *QueryAPI) Describe() query.Vocabulary {
	return query.Describe()
}

// Run answers one query. A spec the console refuses is query_error, the input's fault; a store
// failure is persistence_error, the case's.
//
// A panic is recovered into internal_error. Wails would otherwise answer the call with nothing, the
// runtime cannot parse nothing, and the page would wait on a promise that never settles.
func (a *QueryAPI) Run(spec query.Spec) (res query.Result, err error) {
	defer func() {
		if p := recover(); p != nil {
			res, err = query.Result{}, AsError(consts.ErrCodeInternal, fmt.Errorf(consts.MsgQueryPanic, p))
		}
	}()
	res, err = query.Run(a.reader, spec)
	if err == nil {
		return res, nil
	}
	if _, refused := errors.AsType[*query.RefusedError](err); refused {
		return query.Result{}, AsError(consts.ErrCodeQuery, err)
	}
	return query.Result{}, AsError(consts.ErrCodePersistence, err)
}
