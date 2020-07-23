package api

import (
	"log"
	"os"
)

type NitrodService struct {
	command Runner
	logger  *log.Logger
}

func NewNitrodService() *NitrodService {
	return &NitrodService{
		command: &ServiceRunner{},
		logger:  log.New(os.Stdout, "nitrod ", 0),
	}
}
