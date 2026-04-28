package httpclient

type Classification int

const (
	Success Classification = iota
	Poison
	Halt
	Retry
)

func (c Classification) String() string {
	switch c {
	case Success:
		return "success"
	case Poison:
		return "poison"
	case Halt:
		return "halt"
	case Retry:
		return "retry"
	}
	return "unknown"
}

func Classify(status int, err error) Classification {
	if err != nil {
		return Retry
	}
	switch {
	case status >= 200 && status < 300:
		return Success
	case status == 401:
		return Halt
	case status == 400 || status == 403 || status == 404:
		return Poison
	default:
		return Retry
	}
}
