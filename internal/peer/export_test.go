package peer

// ReadScreen is a test-only export so a test can time readScreen at the phase
// where its budget starts.
var ReadScreen = readScreen

// SendBusyBudgetMS is a test-only export of the busy wait's budget after an
// agent_not_ready wait (DJA-377).
var SendBusyBudgetMS = sendBusyBudgetMS
