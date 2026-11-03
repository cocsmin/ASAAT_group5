// Package model holds the data types shared between services and stores.
package model

import (
	"strings"
	"time"
)

// Group is the Sparkplug group id of the whole factory.
const Group = "CakeFactory"

// Systems of the factory. A system is one or more containers (Sparkplug edge
// nodes): a system that is split per station has one node per station, named
// <SYSTEM>-<STATION>, and the system id itself is only a group name.
const (
	PLM = "PLM"
	BAT = "BAT"
	THM = "THM"
	FIN = "FIN"
	BT  = "BT"
	MON = "MON"
)

// Station nodes of Thermal Processing: one container each.
const (
	THMDep  = "THM-DEP"  // depositing
	THMBake = "THM-BAKE" // baking tunnel, three zones
	THMExit = "THM-EXIT" // tunnel exit check
	THMCool = "THM-COOL" // cooling and its fallback buffer
)

// Station nodes of Finishing & Packaging.
const (
	FINDpn  = "FIN-DPN"  // depanning
	FINQua  = "FIN-QUA"  // quality inspection
	FINWrap = "FIN-WRAP" // wrapping and the holding area in front of it
	FINPal  = "FIN-PAL"  // cartoning, case check and pallets
	FINWash = "FIN-WASH" // pan washing, the clean-pan pool and the pan supply to depositing
)

// Station nodes of Production & Logistics Management.
const (
	PLMWh   = "PLM-WH"   // orders, recipes, warehouse and inventory
	PLMShip = "PLM-SHIP" // transport planning and shipment
)

// Station nodes of Batter Preparation.
const (
	BATDose = "BAT-DOSE" // order intake, ingredient reservation and dosing
	BATMix  = "BAT-MIX"  // mixing
	BATInsp = "BAT-INSP" // human inspection gate and the batter handoff
)

// Nodes lists every edge node (every container that talks Sparkplug) in line order.
var Nodes = []string{PLMWh, PLMShip, BATDose, BATMix, BATInsp, THMDep, THMBake, THMExit, THMCool, FINDpn, FINQua, FINWrap, FINPal, FINWash, BT, MON}

// Systems lists the systems in line order.
var Systems = []string{PLM, BAT, THM, FIN, BT, MON}

// SystemOf returns the system a node belongs to ("THM-BAKE" is part of "THM").
func SystemOf(node string) string {
	s, _, _ := strings.Cut(node, "-")
	return s
}

// StationsOf returns the edge nodes of a system, in line order.
func StationsOf(system string) []string {
	var out []string
	for _, n := range Nodes {
		if SystemOf(n) == system {
			out = append(out, n)
		}
	}
	return out
}

// NodeOfMachine returns the node that a machine id ("thm-bake-zone-2") belongs to:
// the longest node id that is a prefix of it, or fallback.
func NodeOfMachine(machine, fallback string) string {
	best := ""
	for _, n := range Nodes {
		if p := strings.ToLower(n) + "-"; strings.HasPrefix(machine, p) && len(n) > len(best) {
			best = n
		}
	}
	if best == "" {
		return fallback
	}
	return best
}

// The five independently drivable band sections of the Band Transport system and
// the system whose stations sit on each section.
var sectionNames = [6]string{"", "Depositing", "Baking tunnel", "Cooling", "Depanning & inspection", "Packaging"}
var sectionOwners = [6]string{"", THM, THM, THM, FIN, FIN}

// sectionNodes lists the nodes that work on each band section.
var sectionNodes = [6][]string{nil, {THMDep}, {THMBake}, {THMExit, THMCool}, {FINDpn, FINQua}, {FINWrap, FINPal}}

// OwnsSection reports whether node works on band section i (1 to 5).
func OwnsSection(node string, i int) bool {
	for _, n := range sectionNodes[i] {
		if n == node {
			return true
		}
	}
	return false
}

// SectionName returns the name of band section i (1 to 5).
func SectionName(i int) string { return sectionNames[i] }

// SectionOwner returns the system that works on band section i (1 to 5).
func SectionOwner(i int) string { return sectionOwners[i] }

// Cooling. A cake may be packaged once its core is at room temperature (UC10). Cooling brings it
// to CoolMean with a scatter of CoolSigma; TSafe is the limit above which Finishing refuses it.
// Depositing, cooling and depanning all use these values, so they cannot disagree.
const (
	TSafe     = 28.0 // core temperature in C up to which a cooled cake is accepted for packaging
	CoolMean  = 24.0 // core temperature in C after cooling
	CoolSigma = 1.0  // scatter of that temperature
)

// Severities of the event signal.
const (
	SevLog     = "log"
	SevWarning = "warning"
	SevError   = "error"
	SevDiscard = "discard"
)

// Device states reported in health messages.
const (
	StateOK      = "OK"
	StateIdle    = "IDLE"
	StateWarn    = "DEGRADED"
	StateFault   = "FAULT"
	StateOffline = "OFFLINE"
)

// Step is one entry in a pan's travel history.
type Step struct {
	At      time.Time `json:"at" bson:"at"`
	Station string    `json:"station" bson:"station"`
	Status  string    `json:"status" bson:"status"`
	Note    string    `json:"note,omitempty" bson:"note,omitempty"`
}

// Pan is the document that travels from station to station. In MongoDB it is
// one document in the pans collection; without MongoDB (tests) it is one JSON file.
type Pan struct {
	ID         string    `json:"id" bson:"_id"`
	BatchID    string    `json:"batchId" bson:"batchId"`
	PanID      string    `json:"panId,omitempty" bson:"panId,omitempty"` // set on cake documents
	Station    string    `json:"station" bson:"station"`
	Status     string    `json:"status" bson:"status"`
	FillWeight float64   `json:"fillWeight" bson:"fillWeight"`
	CakeTemp   float64   `json:"cakeTemp" bson:"cakeTemp"`
	Updated    time.Time `json:"updated" bson:"updated"`
	History    []Step    `json:"history" bson:"history"`
}

// Item is one ingredient quantity (grams).
type Item struct {
	Ingredient string  `json:"ingredient"`
	Grams      float64 `json:"grams"`
}

// Recipe holds the targets used by dosing, mixing, depositing and inspection.
type Recipe struct {
	Name         string
	Items        []Item
	MixSeconds   int
	DoseTolPct   float64 // dose tolerance P1, percent of target
	PansPerBatch int
	FillTarget   float64 // grams of batter per pan
	FillTol      float64 // grams
}

// Order is a production order.
type Order struct {
	ID      string    `json:"id"`
	Recipe  string    `json:"recipe"`
	Status  string    `json:"status"`
	Created time.Time `json:"created"`
	Note    string    `json:"note"`
}

// StockRow is the stock of one ingredient.
type StockRow struct {
	Ingredient string  `json:"ingredient"`
	Grams      float64 `json:"grams"`
	Reserved   float64 `json:"reserved"`
	Organic    bool    `json:"organic"`
}

// LogEvent is one traceability record in the SQL event log.
type LogEvent struct {
	ID          int64     `json:"id"`
	At          time.Time `json:"at"`
	Node        string    `json:"node"`
	Device      string    `json:"device"`
	Severity    string    `json:"severity"`
	Code        string    `json:"code"`
	Reason      string    `json:"reason"`
	BatchID     string    `json:"batchId"`
	MachineID   string    `json:"machineId"`
	Maintenance bool      `json:"maintenance"`
}

// Ticket is a maintenance ticket.
type Ticket struct {
	ID        int64      `json:"id"`
	Opened    time.Time  `json:"opened"`
	Closed    *time.Time `json:"closed,omitempty"`
	Node      string     `json:"node"`
	MachineID string     `json:"machineId"`
	Reason    string     `json:"reason"`
	Status    string     `json:"status"` // open | repaired
}

// Shipment is a delivered pallet.
type Shipment struct {
	PalletID string    `json:"palletId"`
	Cakes    int       `json:"cakes"`
	Batches  string    `json:"batches"`
	Vehicle  string    `json:"vehicle"`
	At       time.Time `json:"at"`
}
