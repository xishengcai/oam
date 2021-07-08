package canarytrait

import (
	"reflect"

	"istio.io/client-go/pkg/apis/networking/v1alpha3"
)

const (
	canaryTypeTraffic = "traffic"
)

var (
	virtualServiceKind       = reflect.TypeOf(v1alpha3.VirtualService{}).Name()
	virtualServiceAPIVersion = v1alpha3.SchemeGroupVersion.String()

	destinationRuleKind   = reflect.TypeOf(v1alpha3.VirtualService{}).Name()
	destinationAPIVersion = v1alpha3.SchemeGroupVersion.String()
)
