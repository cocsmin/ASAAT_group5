// Package fin is the Finishing & Packaging system, split per station, one
// container each: Depanning (dpn.go), Quality inspection (qua.go), Wrapping with
// its holding area (wrap.go), Cartoning and pallets (pal.go) and Pan washing with
// the clean-pan pool and the pan supply to depositing (wash.go). Cakes move on
// with a signal ("Cake to inspection", "Cake to wrapping", "Cake to cartoning");
// the cake document in MongoDB says where a cake is, so a station that restarts,
// or that missed a message, adopts the cakes the station before it handed over.
package fin

import "time"

const (
	depanTime   = 1 * time.Second
	inspectTime = 1 * time.Second
	wrapTime    = 1 * time.Second
	cartonTime  = 1 * time.Second
	washTime    = 600 * time.Millisecond
	dryTime     = 400 * time.Millisecond
	holdingCap  = 6 // holding area in front of wrapping
	maxRejects  = 3 // consecutive rejects before Maintenance is called

	depannerMachine  = "fin-dpn-depanner"
	washerMachine    = "fin-wash-washer"
	wrapperMachine   = "fin-wrap-wrapper"
	inspectorMachine = "fin-qua-inspector"
)

type cake struct {
	pan, batch string
}

// doc is the id of the cake's own document. After depanning the cake and the
// pan travel separately, so each has its own document.
func (c cake) doc() string { return "CAKE-" + c.batch + "-" + c.pan }
