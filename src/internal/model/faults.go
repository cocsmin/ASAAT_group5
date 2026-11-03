package model

// FaultInfo describes one failure case that can be injected from the dashboard.
type FaultInfo struct {
	Code     string `json:"code"`
	Node     string `json:"node"`
	Title    string `json:"title"`
	Resolved string `json:"resolved"` // System | Maintenance
	Count    int    `json:"count"`    // charges armed by one click
}

// FaultCatalog lists the failure cases (FC) and use-case alternatives (UC)
// from the report that the simulation can inject. Codes match the report.
var FaultCatalog = []FaultInfo{
	{"FC1", PLMWh, "Not enough ingredients in the warehouse", "System", 1},
	{"FC2", PLMWh, "Warehouse system not responding", "System", 1},
	{"FC27", PLMWh, "Controller container crashes", "System", 1},
	{"FC28", PLMWh, "Controller stops responding", "Maintenance", 1},
	{"FC27", PLMShip, "Controller container crashes", "System", 1},
	{"FC28", PLMShip, "Controller stops responding", "Maintenance", 1},

	{"FC3", BATDose, "Dosing: too little of an ingredient", "System", 1},
	{"FC4", BATDose, "Dosing: too much of an ingredient", "System", 1},
	{"FC5", BATDose, "Dosing fails several times in a row", "Maintenance", 3},
	{"FC6", BATMix, "Mixer breaks", "Maintenance", 1},
	{"UC4", BATInsp, "Human inspector rejects the batch", "System", 1},
	{"FC27", BATDose, "Controller container crashes", "System", 1},
	{"FC28", BATDose, "Controller stops responding", "Maintenance", 1},
	{"FC27", BATMix, "Controller container crashes", "System", 1},
	{"FC28", BATMix, "Controller stops responding", "Maintenance", 1},
	{"FC27", BATInsp, "Controller container crashes", "System", 1},
	{"FC28", BATInsp, "Controller stops responding", "Maintenance", 1},

	{"UC5", THMDep, "Weight-Checker out of calibration", "System", 1},
	{"FC7", THMDep, "Depositing stops, batter waits too long", "System", 1},
	{"FC8", THMDep, "Depositing: no pan present", "System", 1},
	{"FC9", THMDep, "Depositing: wrong fill weight", "System", 1},
	{"FC10", THMDep, "Wrong fill weight several times in a row", "Maintenance", 3},
	{"FC11", THMBake, "Baking tunnel: zone temperature wrong", "Maintenance", 1},
	{"FC12", THMExit, "Tunnel exit: cake under- or overcooked", "System", 1},
	{"FC13", THMExit, "Tunnel exit: heat camera broken", "Maintenance", 1},
	{"FC14", THMCool, "Cooling: cake still too hot", "System", 1},
	{"FC15", THMCool, "Cooling stops: cakes go to the buffer (then FC18 buffer full, FC17 cakes wait too long, FC16 repair releases them)", "Maintenance", 1},
	{"FC27", THMDep, "Controller container crashes", "System", 1},
	{"FC28", THMDep, "Controller stops responding", "Maintenance", 1},
	{"FC27", THMBake, "Controller container crashes", "System", 1},
	{"FC28", THMBake, "Controller stops responding", "Maintenance", 1},
	{"FC27", THMExit, "Controller container crashes", "System", 1},
	{"FC28", THMExit, "Controller stops responding", "Maintenance", 1},
	{"FC27", THMCool, "Controller container crashes", "System", 1},
	{"FC28", THMCool, "Controller stops responding", "Maintenance", 1},

	{"FC21", FINDpn, "Depanning: cake stuck or broken", "System", 1},
	{"FC22", FINDpn, "Depanning machine breaks", "Maintenance", 1},
	{"UC14", FINWrap, "Wrapped or cartoned cake defective", "System", 1},
	{"UC12", FINWash, "Washed pan is damaged", "System", 1},
	{"FC23", FINWash, "Washing station breaks", "Maintenance", 1},
	{"FC24", FINQua, "Quality inspection: defect, weight or metal", "System", 1},
	{"FC24", FINQua, "Same quality check fails 3 times in a row", "Maintenance", 3},
	{"FC25", FINWrap, "Wrapping station breaks", "Maintenance", 1},
	{"FC26", FINPal, "Cartoning: wrong case weight", "System", 1},
	{"FC27", FINDpn, "Controller container crashes", "System", 1},
	{"FC28", FINDpn, "Controller stops responding", "Maintenance", 1},
	{"FC27", FINQua, "Controller container crashes", "System", 1},
	{"FC28", FINQua, "Controller stops responding", "Maintenance", 1},
	{"FC27", FINWrap, "Controller container crashes", "System", 1},
	{"FC28", FINWrap, "Controller stops responding", "Maintenance", 1},
	{"FC27", FINPal, "Controller container crashes", "System", 1},
	{"FC28", FINPal, "Controller stops responding", "Maintenance", 1},
	{"FC27", FINWash, "Controller container crashes", "System", 1},
	{"FC28", FINWash, "Controller stops responding", "Maintenance", 1},

	{"FC19", BT, "Band section stops", "Maintenance", 1},
	{"FC20", BT, "Band runs at the wrong speed", "System", 1},
	{"FC20", BT, "Band wrong speed, correction fails", "Maintenance", 2},
	{"FC27", BT, "Controller container crashes", "System", 1},
	{"FC28", BT, "Controller stops responding", "Maintenance", 1},
}
