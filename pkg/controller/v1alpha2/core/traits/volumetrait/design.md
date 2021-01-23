# volumeTrait design

## 1. crd
```go
type VolumeTrait struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VolumeTraitSpec   `json:"spec,omitempty"`
	Status VolumeTraitStatus `json:"status,omitempty"`
}

// A VolumeTraitSpec defines the desired state of a VolumeTrait.
type VolumeTraitSpec struct {
	VolumeList []VolumeMountItem `json:"volumeList"`
	// WorkloadReference to the workload this trait applies to.
	WorkloadReference runtimev1alpha1.TypedReference `json:"workloadRef"`
}

type VolumeMountItem struct {
	ContainerIndex int        `json:"containerIndex"`
	Paths          []PathItem `json:"paths"`
}

type PathItem struct {
	StorageClassName string             `json:"storageClassName"`
	Size             string             `json:"size"`
	Path             string             `json:"path"`
}
```
        apply deployment

## 2. 功能目标
1. oam ContainerizedWorkload child resource 支持 动态工作负载，原先只支持deployment，现在
是在第一次初始化的工作负载的时候，发现如果添加了 volume trait，则更改工作负载为statefulSet，否则默认是deployment。

2. 用户填写信息
- 存储类名称
- 存储大小
- 容器内挂载路径
    

## 3. 实现逻辑
1. appConfig 
- set workload label: if find trait volume, set label   app.oam.dev/childResource=StatefulSet 
- apply workload

多态只在第一次apply container workload 的时候，决定子负载类型，后面增删volumeTrait不会影响子负载类型。

2. container workload
- apply childResource [deployment|statefulSet, service]
    if label  app.oam.dev/childResource==StatefulSet,
    else
        apply statefulSet
      
2. volumeTrait
- fetchWorkloadChildResources by traits 
- create pvc by storageClassName and size 
- patch Volumes and VolumeMount

## 4. 注意事项
1. nfs,nas 大小无法限制
2. pvc 创建后不能修改大小
3. VolumeTrait 只支持重建，不支持修改
    
## pvc create

### 支持 storage class update
1. get pvc
2. 比较pvc. storageclass
   no delete old
   create new pvc
   
   new pvc uid list
3. gc
   compare pvcUid && volumeTrait status resource  pvc
   if pvc.uid not in pvcUid
        delete


