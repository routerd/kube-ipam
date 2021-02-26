/*
Copyright 2021 The routerd authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"os"

	"github.com/metal-stack/go-ipam"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
	"routerd.net/kube-ipam/internal/controllers"
	"routerd.net/kube-ipam/internal/controllers/adapter"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = ipamv1alpha1.AddToScheme(scheme)
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "32bf6c51.routerd.net",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ipamCache := controllers.NewIPAMCache()

	var (
		ipv4PoolType      = adapter.AdaptIPPool(&ipamv1alpha1.IPv4Pool{})
		ipv4LeaseType     = adapter.AdaptIPLease(&ipamv1alpha1.IPv4Lease{})
		ipv4LeaseListType = adapter.AdaptIPLeaseList(&ipamv1alpha1.IPv4LeaseList{})

		ipv6PoolType      = adapter.AdaptIPPool(&ipamv1alpha1.IPv6Pool{})
		ipv6LeaseType     = adapter.AdaptIPLease(&ipamv1alpha1.IPv6Lease{})
		ipv6LeaseListType = adapter.AdaptIPLeaseList(&ipamv1alpha1.IPv6LeaseList{})
	)

	// IPv4 Controllers
	if err = (&controllers.IPPoolReconciler{
		Client:    mgr.GetClient(),
		Log:       ctrl.Log.WithName("controllers").WithName("IPv4Pool"),
		Scheme:    mgr.GetScheme(),
		IPAMCache: ipamCache,
		NewIPAM:   func() controllers.Ipamer { return ipam.New() },

		IPPoolType:      ipv4PoolType,
		IPLeaseType:     ipv4LeaseType,
		IPLeaseListType: ipv4LeaseListType,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "IPv4Pool")
		os.Exit(1)
	}
	if err = (&controllers.IPLeaseReconciler{
		Client:    mgr.GetClient(),
		Log:       ctrl.Log.WithName("controllers").WithName("IPv4Lease"),
		Scheme:    mgr.GetScheme(),
		IPAMCache: ipamCache,

		IPPoolType:  ipv4PoolType,
		IPLeaseType: ipv4LeaseType,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "IPv4Lease")
		os.Exit(1)
	}

	// IPv6 Controllers
	if err = (&controllers.IPPoolReconciler{
		Client:    mgr.GetClient(),
		Log:       ctrl.Log.WithName("controllers").WithName("IPv6Pool"),
		Scheme:    mgr.GetScheme(),
		IPAMCache: ipamCache,
		NewIPAM:   func() controllers.Ipamer { return ipam.New() },

		IPPoolType:      ipv6PoolType,
		IPLeaseType:     ipv6LeaseType,
		IPLeaseListType: ipv6LeaseListType,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "IPv6Pool")
		os.Exit(1)
	}
	if err = (&controllers.IPLeaseReconciler{
		Client:    mgr.GetClient(),
		Log:       ctrl.Log.WithName("controllers").WithName("IPv6Lease"),
		Scheme:    mgr.GetScheme(),
		IPAMCache: ipamCache,

		IPPoolType:  ipv6PoolType,
		IPLeaseType: ipv6LeaseType,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "IPv6Lease")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
