package exec

type registryState uint8

const (
	registryStateActive registryState = iota + 1
	registryStateClosing
)
