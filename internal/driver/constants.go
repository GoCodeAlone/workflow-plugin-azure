package driver

import "time"

// pollFrequency is the default polling interval for Azure long-running operations.
const pollFrequency = 5 * time.Second
