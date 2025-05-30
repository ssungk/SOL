package rtmp

type client struct {
	channel chan<- interface{}
}

func NewClient() *client {
	c := &client{}
	return c
}
