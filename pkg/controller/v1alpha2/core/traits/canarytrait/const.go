package canarytrait

import (
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"reflect"
)

const (
	canaryTypeTraffic = "traffic"
	canaryTypeHeader  = "header"
)

var (
	virtualServiceKind       = reflect.TypeOf(v1alpha3.VirtualService{}).Name()
	virtualServiceAPIVersion = v1alpha3.SchemeGroupVersion.String()

	destinationRuleKind   = reflect.TypeOf(v1alpha3.VirtualService{}).Name()
	destinationAPIVersion = v1alpha3.SchemeGroupVersion.String()
)
