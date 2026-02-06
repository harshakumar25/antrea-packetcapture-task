package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/harshakumar25/packetcapture-controller/pkg/capture"
)

const annotationKey = "tcpdump.antrea.io"

func main() {
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		slog.Error("NODE_NAME environment variable not set")
		os.Exit(1)
	}

	slog.Info("Starting packet capture controller", "node", nodeName)

	config, err := rest.InClusterConfig()
	if err != nil {
		slog.Error("Failed to get in-cluster config", "error", err)
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		slog.Error("Failed to create clientset", "error", err)
		os.Exit(1)
	}

	manager := capture.NewManager()

	// Create informer factory with field selector for local node only
	factory := informers.NewSharedInformerFactoryWithOptions(
		clientset,
		0,
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.FieldSelector = fields.OneTermEqualSelector("spec.nodeName", nodeName).String()
		}),
	)

	podInformer := factory.Core().V1().Pods().Informer()

	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)
			handlePod(pod, manager)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			pod := newObj.(*corev1.Pod)
			handlePod(pod, manager)
		},
		DeleteFunc: func(obj interface{}) {
			pod := extractPod(obj)
			if pod == nil {
				return
			}
			if manager.IsCapturing(pod.Name) {
				slog.Info("Pod deleted, stopping capture", "pod", pod.Name, "namespace", pod.Namespace)
				manager.StopCapture(pod.Name)
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		slog.Info("Received shutdown signal")
		cancel()
	}()

	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	slog.Info("Controller started, watching pods")
	<-ctx.Done()
	slog.Info("Controller shutting down")
	manager.StopAll()
}

func handlePod(pod *corev1.Pod, manager *capture.Manager) {
	annotation, exists := pod.Annotations[annotationKey]

	if exists && !manager.IsCapturing(pod.Name) {
		// Parse max files from annotation
		maxFiles, err := strconv.Atoi(annotation)
		if err != nil || maxFiles <= 0 {
			maxFiles = 1
		}
		slog.Info("Starting capture", "pod", pod.Name, "namespace", pod.Namespace, "maxFiles", maxFiles)
		manager.StartCapture(pod.Name, maxFiles)
	} else if !exists && manager.IsCapturing(pod.Name) {
		slog.Info("Annotation removed, stopping capture", "pod", pod.Name, "namespace", pod.Namespace)
		manager.StopCapture(pod.Name)
	}
}

func extractPod(obj interface{}) *corev1.Pod {
	switch t := obj.(type) {
	case *corev1.Pod:
		return t
	case cache.DeletedFinalStateUnknown:
		if pod, ok := t.Obj.(*corev1.Pod); ok {
			return pod
		}
	}
	return nil
}
