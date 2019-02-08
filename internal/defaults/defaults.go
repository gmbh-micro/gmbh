package defaults

const (
	VERSION           = "0.8.2"
	CODE              = "handshake"
	CORE_PATH_MAC     = "/usr/local/bin/gmbhCore"
	CORE_PATH_LINUX   = ""
	CORE_PATH_WINDOWS = ""
	CLI_PROMPT        = "[cli] "
	CTRL_PROMPT       = "[gmbh] "
	DEFAULT_PROMPT    = "[gmbh] "
)

// For use with UserConfig
const (
	PROJECT_NAME        = "default"
	PROJECT_CONFIG_FILE = "gmbh.yaml"
	DAEMON              = false
	VERBOSE             = true
	DEFAULT_HOST        = "localhost"
	DEFAULT_PORT        = ":59999"
	CONTROL_HOST        = "localhost"
	CONTROL_PORT        = ":59997"
)

// For use with ServiceConfig
const (
	SERVICE_NAME = "default"
	IS_CLIENT    = true
	IS_SERVER    = true
)

// For use with services
const (
	CONFIG_FILE      = "/gmbh.yaml"
	CONFIG_FILE_EXT  = ".yaml"
	SERVICE_LOG_PATH = "/gmbh/"
	SERVICE_LOG_FILE = "core.log"
)

// For use with router
const (
	BASE_ADDRESS = "localhost"
	BASE_PORT    = 49999
)

// For use with process manager
const (
	STARTING_ID = 100
	NUM_RETRIES = 3
	TIMEOUT     = 30
)