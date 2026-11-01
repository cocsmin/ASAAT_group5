// Package model holds the data types shared between services and stores.
package model

import "time"

// Group is the Sparkplug group id of the whole factory.
const Group = "CakeFactory"

// Node ids (Sparkplug edge nodes), one per system.
const (
	PLM = "PLM"
	BAT = "BAT"
	THM = "THM"
	FIN = "FIN"
	MON = "MON"
)

// Nodes lists every edge node in line order.
var Nodes = []string{PLM, BAT, THM, FIN, MON}

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
// one document in the pans collection; in dev mode it is one JSON file.
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
