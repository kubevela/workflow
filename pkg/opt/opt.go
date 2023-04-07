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
	providers       types.Providers
	packageDiscover *packages.PackageDiscover
	processCtx      process.Context
	templateLoader  template.Loader
	client          client.Client
	stepConvertor   map[string]func(step v1alpha1.WorkflowStep) (v1alpha1.WorkflowStep, error)
	logLevel        int
}

func NewStepGeneratorOptions(client client.Client, instance *types.WorkflowInstance, opts ...Option) types.StepGeneratorOptions {
	o := StepGeneratorOptions{
		client:         client,
		providers:      providers.NewProviders(),
		templateLoader: template.NewWorkflowStepTemplateLoader(client),
		logLevel:       3,
	}
	for _, opt := range opts {
		opt(&o)
	}

	installBuiltinProviders(instance, client, o.providers, o.processCtx)

	return types.StepGeneratorOptions{
		Providers:       o.providers,
		PackageDiscover: o.packageDiscover,
		ProcessCtx:      o.processCtx,
		TemplateLoader:  o.templateLoader,
		Client:          o.client,
		StepConvertor:   o.stepConvertor,
		LogLevel:        o.logLevel,
	}
}

func WithProviders(providers types.Providers) Option {
	return func(o *StepGeneratorOptions) {
		o.providers = providers
	}
}

func WithPackageDiscover(discover *packages.PackageDiscover) Option {
	return func(o *StepGeneratorOptions) {
		o.packageDiscover = discover
	}
}

func WithProcessCtx(ctx process.Context) Option {
	return func(o *StepGeneratorOptions) {
		o.processCtx = ctx
	}
}

func WithTemplateLoader(loader template.Loader) Option {
	return func(o *StepGeneratorOptions) {
		o.templateLoader = loader
	}
}

func WithClient(c client.Client) Option {
	return func(o *StepGeneratorOptions) {
		o.client = c
	}
}

func WithStepConvertor(convertor map[string]func(step v1alpha1.WorkflowStep) (v1alpha1.WorkflowStep, error)) Option {
	return func(o *StepGeneratorOptions) {
		o.stepConvertor = convertor
	}
}

func WithLogLevel(l int) Option {
	return func(o *StepGeneratorOptions) {
		o.logLevel = l
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
