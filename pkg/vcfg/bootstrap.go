package vcfg

type Command string

// Bootstrap commands
const (
	_                       Command = "BootstrapCommand"
	BootstrapWaitFile               = "WAIT_FILE"
	BootstrapWaitPort               = "WAIT_PORT"
	BootstrapSleep                  = "SLEEP"
	BootstrapFindAndReplace         = "FIND_AND_REPLACE"
)
