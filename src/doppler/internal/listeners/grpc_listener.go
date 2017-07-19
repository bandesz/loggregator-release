package listeners

import (
	"diodes"
	"doppler/app"
	"doppler/internal/grpcmanager/v1"
	"doppler/internal/grpcmanager/v2"
	"doppler/internal/sinkserver/sinkmanager"
	"fmt"
	"log"
	"metricemitter"
	"net"
	plumbingv1 "plumbing"
	plumbingv2 "plumbing/v2"
	"time"

	"github.com/cloudfoundry/dropsonde/metricbatcher"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

type GRPCListener struct {
	listener net.Listener
	server   *grpc.Server
}

func NewGRPCListener(
	reg v1.Registrar,
	sinkmanager *sinkmanager.SinkManager,
	conf app.GRPC,
	envelopeBuffer *diodes.ManyToOneEnvelope,
	batcher *metricbatcher.MetricBatcher,
	metricClient metricemitter.MetricClient,
) (*GRPCListener, error) {
	var opts []plumbingv1.ConfigOption
	if len(conf.CipherSuites) > 0 {
		opts = append(opts, plumbingv1.WithCipherSuites(conf.CipherSuites))
	}

	tlsConfig, err := plumbingv1.NewServerMutualTLSConfig(
		conf.CertFile,
		conf.KeyFile,
		conf.CAFile,
		opts...,
	)
	if err != nil {
		return nil, err
	}
	transportCreds := credentials.NewTLS(tlsConfig)

	log.Printf("Listening for GRPC connections on %d", conf.Port)
	grpcListener, err := net.Listen("tcp", fmt.Sprintf(":%d", conf.Port))

	if err != nil {
		log.Printf("Failed to start listener (port=%d) for gRPC: %s", conf.Port, err)
		return nil, err
	}

	kp := keepalive.EnforcementPolicy{
		MinTime:             10 * time.Second,
		PermitWithoutStream: true,
	}

	grpcServer := grpc.NewServer(grpc.Creds(transportCreds), grpc.KeepaliveEnforcementPolicy(kp))

	// v1 ingress
	plumbingv1.RegisterDopplerIngestorServer(
		grpcServer,
		v1.NewIngestorServer(envelopeBuffer, batcher),
	)
	// v1 egress
	plumbingv1.RegisterDopplerServer(
		grpcServer,
		v1.NewDopplerServer(reg, sinkmanager, metricClient),
	)

	// v2 ingress
	plumbingv2.RegisterDopplerIngressServer(
		grpcServer,
		v2.NewIngressServer(envelopeBuffer, batcher, metricClient),
	)

	return &GRPCListener{
		listener: grpcListener,
		server:   grpcServer,
	}, nil
}

func (g *GRPCListener) Start() {
	log.Printf("Starting gRPC server on %s", g.listener.Addr().String())
	if err := g.server.Serve(g.listener); err != nil {
		log.Fatalf("Failed to start gRPC server: %s", err)
	}
}
