type Server struct {
	generator worker.Generator
}

func NewServer(g worker.Generator) *Server {
	return &Server{g}
}