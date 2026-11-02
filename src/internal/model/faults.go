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
	{"FC1", PLM, "Not enough ingredients in the warehouse", "System", 1},
	{"FC2", PLM, "Warehouse system not responding", "System", 1},
	{"FC27", PLM, "Controller container crashes", "System", 1},
	{"FC28", PLM, "Controller stops responding", "Maintenance", 1},

	{"FC3", BAT, "Dosing: too little of an ingredient", "System", 1},
	{"FC4", BAT, "Dosing: too much of an ingredient", "System", 1},
	{"FC5", BAT, "Dosing fails several times in a row", "Maintenance", 3},
	{"FC6", BAT, "Mixer breaks", "Maintenance", 1},
	{"UC4", BAT, "Human inspector rejects the batch", "System", 1},
	{"FC27", BAT, "Controller container crashes", "System", 1},
	{"FC28", BAT, "Controller stops responding", "Maintenance", 1},

	{"UC5", THM, "Weight-Checker out of calibration", "System", 1},
	{"FC7", THM, "Depositing stops, batter waits too long", "System", 1},
	{"FC8", THM, "Depositing: no pan present", "System", 1},
	{"FC9", THM, "Depositing: wrong fill weight", "System", 1},
	{"FC10", THM, "Wrong fill weight several times in a row", "Maintenance", 3},
	{"FC11", THM, "Baking tunnel: zone temperature wrong", "Maintenance", 1},
	{"FC12", THM, "Tunnel exit: cake under- or overcooked", "System", 1},
	{"FC13", THM, "Tunnel exit: heat camera broken", "Maintenance", 1},
	{"FC14", THM, "Cooling: cake still too hot", "System", 1},
	{"FC15", THM, "Cooling stops: cakes go to the buffer (then FC18 buffer full, FC17 cakes wait too long, FC16 repair releases them)", "Maintenance", 1},
	{"FC27", THM, "Controller container crashes", "System", 1},
	{"FC28", THM, "Controller stops responding", "Maintenance", 1},

	{"FC21", FIN, "Depanning: cake stuck or broken", "System", 1},
	{"FC22", FIN, "Depanning machine breaks", "Maintenance", 1},
	{"UC14", FIN, "Wrapped or cartoned cake defective", "System", 1},
	{"UC12", FIN, "Washed pan is damaged", "System", 1},
	{"FC23", FIN, "Washing station breaks", "Maintenance", 1},
	{"FC24", FIN, "Quality inspection: defect, weight or metal", "System", 1},
	{"FC24", FIN, "Same quality check fails 3 times in a row", "Maintenance", 3},
	{"FC25", FIN, "Wrapping station breaks", "Maintenance", 1},
	{"FC26", FIN, "Cartoning: wrong case weight", "System", 1},
	{"FC27", FIN, "Controller container crashes", "System", 1},
	{"FC28", FIN, "Controller stops responding", "Maintenance", 1},

	{"FC19", MON, "Band section stops", "Maintenance", 1},
	{"FC20", MON, "Band runs at the wrong speed", "System", 1},
	{"FC20", MON, "Band wrong speed, correction fails", "Maintenance", 2},
}
