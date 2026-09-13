package worker

type Request struct {
	Model           string
	Prompt          string
	MaxOutputTokens int
	Temperature     float64
}

type Token struct {
	Text string
	Err  error
}
