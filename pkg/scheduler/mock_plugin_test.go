package scheduler

import (
	"fmt"

	"github.com/apache/incubator-yunikorn-core/pkg/log"
	"github.com/apache/incubator-yunikorn-scheduler-interface/lib/go/si"
	"go.uber.org/zap"
)

type fakePredicatePlugin struct {
	mustFail bool
	nodes    map[string]int
}

func (f *fakePredicatePlugin) Predicates(args *si.PredicatesArgs) error {
	if f.mustFail {
		log.Logger().Info("fake predicate plugin fail: must fail set")
		return fmt.Errorf("fake predicate plugin failed")
	}
	if fail, ok := f.nodes[args.NodeID]; ok {
		if args.Allocate && fail >= 0 {
			log.Logger().Info("fake predicate plugin node allocate fail",
				zap.String("node", args.NodeID),
				zap.Int("fail mode", fail))
			return fmt.Errorf("fake predicate plugin failed")
		}
		if !args.Allocate && fail <= 0 {
			log.Logger().Info("fake predicate plugin node reserve fail",
				zap.String("node", args.NodeID),
				zap.Int("fail mode", fail))
			return fmt.Errorf("fake predicate plugin failed")
		}
	}
	log.Logger().Info("fake predicate plugin pass",
		zap.String("node", args.NodeID))
	return nil
}

// A fake predicate plugin that can either always fail or fail based on the node that is checked.
// mustFail will cause the predicate check to always fail
// nodes allows specifying which node to fail for which check using the nodeID:
// possible values: -1 fail reserve, 0 fail always, 1 fail alloc (defaults to always)
func newFakePredicatePlugin(mustFail bool, nodes map[string]int) *fakePredicatePlugin {
	return &fakePredicatePlugin{
		mustFail: mustFail,
		nodes:    nodes,
	}
}
