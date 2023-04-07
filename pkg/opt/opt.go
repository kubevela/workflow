package opt

import (
	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/cue/packages"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/providers"
	"github.com/kubevela/workflow/pkg/providers/email"
	"github.com/kubevela/workflow/pkg/providers/http"
	"github.com/kubevela/workflow/pkg/providers/kube"
	metrics2 "github.com/kubevela/workflow/pkg/providers/metrics"
	"github.com/kubevela/workflow/pkg/providers/util"
	"github.com/kubevela/workflow/pkg/providers/workspace"
	"github.com/kubevela/workflow/pkg/tasks/template"
	"github.com/kubevela/workflow/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/oam-dev/kubevela/pkg/config/provider"
)

type Option func(options *StepGeneratorOptions)

// StepGeneratorOptions is the options for generate step.
type StepGeneratorOptions struct {
	Providers       types.Providers
	PackageDiscover *packages.PackageDiscover
	ProcessCtx      process.Context
	TemplateLoader  template.Loader
	Client          client.Client
	StepConvertor   map[string]func(step v1alpha1.WorkflowStep) (v1alpha1.WorkflowStep, error)
	LogLevel        int
}

func NewStepGeneratorOptions(client client.Client, instance *types.WorkflowInstance, opts ...Option) types.StepGeneratorOptions {
	o := StepGeneratorOptions{
		Client:         client,
		Providers:      providers.NewProviders(),
		TemplateLoader: template.NewWorkflowStepTemplateLoader(client),
		LogLevel:       3,
	}
	for _, opt := range opts {
		opt(&o)
	}

	installBuiltinProviders(instance, client, o.Providers, o.ProcessCtx)

	return types.StepGeneratorOptions{
		Providers:       o.Providers,
		PackageDiscover: o.PackageDiscover,
		ProcessCtx:      o.ProcessCtx,
		TemplateLoader:  o.TemplateLoader,
		Client:          o.Client,
		StepConvertor:   o.StepConvertor,
		LogLevel:        o.LogLevel,
	}
}

func WithProviders(providers types.Providers) Option {
	return func(o *StepGeneratorOptions) {
		o.Providers = providers
	}
}

func WithPackageDiscover(discover *packages.PackageDiscover) Option {
	return func(o *StepGeneratorOptions) {
		o.PackageDiscover = discover
	}
}

func WithProcessCtx(ctx process.Context) Option {
	return func(o *StepGeneratorOptions) {
		o.ProcessCtx = ctx
	}
}

func WithTemplateLoader(loader template.Loader) Option {
	return func(o *StepGeneratorOptions) {
		o.TemplateLoader = loader
	}
}

func WithClient(c client.Client) Option {
	return func(o *StepGeneratorOptions) {
		o.Client = c
	}
}

func WithStepConvertor(convertor map[string]func(step v1alpha1.WorkflowStep) (v1alpha1.WorkflowStep, error)) Option {
	return func(o *StepGeneratorOptions) {
		o.StepConvertor = convertor
	}
}

func WithLogLevel(l int) Option {
	return func(o *StepGeneratorOptions) {
		o.LogLevel = l
	}
}

func installBuiltinProviders(instance *types.WorkflowInstance, client client.Client, providerHandlers types.Providers, pCtx process.Context) {
	workspace.Install(providerHandlers, pCtx)
	email.Install(providerHandlers)
	util.Install(providerHandlers, pCtx)
	http.Install(providerHandlers, client, instance.Namespace)
	provider.Install(providerHandlers, client, nil)
	metrics2.Install(providerHandlers)
	kube.Install(providerHandlers, client, map[string]string{
		types.LabelWorkflowRunName:      instance.Name,
		types.LabelWorkflowRunNamespace: instance.Namespace,
	}, nil)
}
