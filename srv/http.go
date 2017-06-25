package srv

import (
	"fmt"
	"net/http"
	"time"

	graceful "gopkg.in/tylerb/graceful.v1"

	"github.com/allegro/bigcache"
	"github.com/s4z/prom2click/config"
	"github.com/s4z/prom2click/db"
	"github.com/s4z/prom2click/workers"
)

type Server struct {
	cache   *bigcache.BigCache
	writer  *workers.WriteWorker
	mux     *http.ServeMux
	listen  string
	timeout time.Duration
}

func NewServer(cfg config.Provider) (*Server, error) {
	var err error
	s := new(Server)
	// Cache of metric hashes
	s.cache, err = db.NewCache(cfg)
	if err != nil {
		return nil, err
	}
	// worker to process remote.WriteRequest
	s.writer, err = workers.NewWriteWorker(s.cache, cfg)
	if err != nil {
		return nil, err
	}
	// http server
	s.mux = http.NewServeMux()
	s.listen = cfg.GetString("http.listen")
	s.timeout = cfg.GetDuration("http.timeout")

	// http handler for remote.WriteRequest
	s.mux.HandleFunc(cfg.GetString("http.write"), func(w http.ResponseWriter, r *http.Request) {
		err := s.writer.Queue(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	})

	// http handler for remote.ReadRequest - todo
	s.mux.HandleFunc(cfg.GetString("http.read"), func(w http.ResponseWriter, r *http.Request) {
		err := fmt.Errorf("todo")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	})

	// todo: http handler for metrics

	return s, nil
}

func (s *Server) Run() error {
	s.writer.Run()
	fmt.Printf("Listening on %s\n", s.listen)
	return graceful.RunWithErr(s.listen, s.timeout, s.mux)
}

func (s *Server) Shutdown() {
	s.writer.Stop()

	// use this for a timeout on writer.Wait()
	wchan := make(chan struct{})
	go func() {
		s.writer.Wait()
		close(wchan)
	}()

	// now wait for writer to stop or a timeout
	select {
	case <-wchan:
		fmt.Println("Clean shutdown..")
	case <-time.After(10 * time.Second):
		fmt.Println("Shutdown timeout, some samples lost..")
	}

}
