// Package proxy provides the main proxy server.
package proxy

import "github.com/ethereum/go-ethereum/common"

type BuilderInfo struct {
	Cert            []byte
	OrderflowSigner common.Address
}

type BuilderConfigHub interface {
	PublishConfig(info BuilderInfo) error
	GetBuilders() ([]BuilderInfo, error)
}

type MockBuilderConfigHub struct{}

func (m MockBuilderConfigHub) PublishConfig(info BuilderInfo) error {
	return nil
}

func (m MockBuilderConfigHub) GetBuilders() ([]BuilderInfo, error) {
	return nil, nil
}
