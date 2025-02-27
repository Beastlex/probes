/*
Copyright 2025.

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

package controller

import (
	"context"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	testv1alpha1 "probes/api/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	WebCheckerWorkersCount           = 2
	WebCheckerProbeTaskChannelSize   = 100
	WebCheckerProbeResultChannelSize = 100
)

type WebCheckerState struct {
	Host          string
	Path          string
	LastCheckTime time.Time
	IsSuccessful  bool
	LastError     string
}

type WebCheckerProbeTask struct {
	NamespacedName types.NamespacedName
	Host           string
	Path           string
}

type WebCheckerProbeResult struct {
	NamespacedName types.NamespacedName
	IsSuccessful   bool
	LastError      string
}

// WebCheckerReconciler reconciles a WebChecker object
type WebCheckerReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	events            chan event.GenericEvent
	mu                sync.RWMutex
	webCheckersStates map[types.NamespacedName]WebCheckerState
	// канал для задач на проверку
	tasks chan WebCheckerProbeTask
	// канал для результатов проверки
	tasksResults chan WebCheckerProbeResult
	cancelFunc   context.CancelFunc
}

// +kubebuilder:rbac:groups=test.network.io,resources=webcheckers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=test.network.io,resources=webcheckers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=test.network.io,resources=webcheckers/finalizers,verbs=update

func (r *WebCheckerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling WebChecker", "request", req)

	var reconciledResource testv1alpha1.WebChecker
	// Вместо ресурса в метод Reconcile приходит запрос с указанием namespace/name ресурса,
	// поэтому нужно запросить ресурс из API кластера
	if err := r.Get(ctx, req.NamespacedName, &reconciledResource); err != nil {
		logger.Error(err, "Failed to get WebChecker resource")
		if errors.IsNotFound(err) {
			r.mu.Lock()
			delete(r.webCheckersStates, req.NamespacedName)
			r.mu.Unlock()
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Если ресурс помечен на удаление, то удаляем запись о нем из контроллера,
	// чтобы по нему не формировались задачи на проверку
	if reconciledResource.DeletionTimestamp != nil {
		logger.Info("WebChecker resource is being deleted")
		r.mu.Lock()
		delete(r.webCheckersStates, req.NamespacedName)
		r.mu.Unlock()
		return ctrl.Result{}, nil
	}

	// Если ресурс создан в кластере, то добавляем его в список проверяемых ресурсов
	r.mu.Lock()
	defer r.mu.Unlock()
	var cachedState WebCheckerState
	var ok bool
	if cachedState, ok = r.webCheckersStates[req.NamespacedName]; !ok {
		r.webCheckersStates[req.NamespacedName] = WebCheckerState{
			Host: reconciledResource.Spec.Host,
			Path: reconciledResource.Spec.Path,
		}
		return ctrl.Result{}, nil
	}

	if cachedState.Host != reconciledResource.Spec.Host || cachedState.Path != reconciledResource.Spec.Path {
		r.webCheckersStates[req.NamespacedName] = WebCheckerState{
			Host: reconciledResource.Spec.Host,
			Path: reconciledResource.Spec.Path,
		}
		return ctrl.Result{}, nil
	}

	// Если запрос пришел из канала событий, то необходимо обновить статус ресурса
	webCheckerStatus := testv1alpha1.WebCheckerStatus{
		Conditions: []metav1.Condition{
			statusFromWebCheckerState(cachedState),
		},
	}
	reconciledResource.Status = webCheckerStatus
	err := r.Status().Update(ctx, &reconciledResource)
	if err != nil {
		logger.Error(err, "Failed to update WebChecker status")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// Формирует статус ресурса на основе результата проверки
func statusFromWebCheckerState(webCheckerState WebCheckerState) metav1.Condition {
	cond := metav1.Condition{
		LastTransitionTime: metav1.Now(),
		Type:               "Ready",
		Reason:             "WebResourceReady",
	}
	if webCheckerState.IsSuccessful {
		cond.Status = metav1.ConditionTrue
		cond.Message = "WebChecker is ready"
	} else {
		cond.Status = metav1.ConditionFalse
		cond.Message = webCheckerState.LastError
	}
	return cond
}

// SetupWithManager sets up the controller with the Manager.
func (r *WebCheckerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&testv1alpha1.WebChecker{}).
		Named("webchecker").
		// Именно WatchesRawSource отвечает за обработку "сторонних событий"
		WatchesRawSource(source.Channel(r.events, &handler.EnqueueRequestForObject{})).
		Complete(r)
}

// Воркер, который выполняет проверку
func (r *WebCheckerReconciler) RunTasksWorker(ctx context.Context) {
	logger := log.FromContext(ctx)
	logger.Info("Starting tasks worker")
	for task := range r.tasks {
		logger.Info("Processing task", "task", task)
		checker := WebCheckerProbe{
			NamespacedName: task.NamespacedName,
			Host:           task.Host,
			Path:           task.Path,
		}
		taskResult := checker.PerformCheck(ctx)
		r.tasksResults <- taskResult
	}
}

// Запускаем все горутины, которые будут формировать задачи на проверку,
// выполнять проверку и обрабатывать результаты
func (r *WebCheckerReconciler) RunWorkers(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	r.cancelFunc = cancel

	go r.runTasksScheduler(ctx)

	for i := 0; i < WebCheckerWorkersCount; i++ {
		go r.RunTasksWorker(ctx)
	}

	go r.runTaskResultsAnalyzer(ctx)
}

// Анализирует результаты проверки и
func (r *WebCheckerReconciler) runTaskResultsAnalyzer(ctx context.Context) {
	logger := log.FromContext(ctx)
	logger.Info("Starting task results analyzer")
	for result := range r.tasksResults {
		r.mu.Lock()
		if resultDetail, ok := r.webCheckersStates[result.NamespacedName]; !ok {
			logger.Info("WebChecker resource not found", "namespacedName", result.NamespacedName)
		} else {
			resultDetail.LastCheckTime = time.Now()
			resultDetail.IsSuccessful = result.IsSuccessful
			resultDetail.LastError = result.LastError
			r.webCheckersStates[result.NamespacedName] = resultDetail
			// Отправляем событие в канал, чтобы контроллер в reconcile обновил статус ресурса
			r.events <- event.GenericEvent{
				Object: &testv1alpha1.WebChecker{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: result.NamespacedName.Namespace,
						Name:      result.NamespacedName.Name,
					},
				},
			}
		}
		r.mu.Unlock()
	}
}

func (r *WebCheckerReconciler) runTasksScheduler(ctx context.Context) {
	logger := log.FromContext(ctx)
	logger.Info("Starting tasks scheduler")
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.mu.RLock()
			for namespacedName, state := range r.webCheckersStates {
				now := time.Now()
				// Если ресурс не проверялся более 10 секунд, то формируем задачу на проверку
				if now.Sub(state.LastCheckTime) > 10*time.Second {
					r.tasks <- WebCheckerProbeTask{
						NamespacedName: namespacedName,
						Host:           state.Host,
						Path:           state.Path,
					}
				}
			}
			r.mu.RUnlock()
		case <-ctx.Done():
			return
		}
	}
}

func NewWebChecker(client client.Client, scheme *runtime.Scheme) *WebCheckerReconciler {
	return &WebCheckerReconciler{
		Client:            client,
		Scheme:            scheme,
		webCheckersStates: make(map[types.NamespacedName]WebCheckerState),
		events:            make(chan event.GenericEvent),
		tasks:             make(chan WebCheckerProbeTask, WebCheckerProbeTaskChannelSize),
		tasksResults:      make(chan WebCheckerProbeResult, WebCheckerProbeResultChannelSize),
	}
}
