# ModelInfo

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Provider** | **string** |  | 
**Model** | **string** |  | 
**FullName** | **string** |  | 
**BaseUrl** | **string** |  | 
**ContextLimit** | **int32** |  | 
**MaxOutput** | **int32** |  | 
**Capabilities** | **NullableString** |  | 
**IsDefault** | **bool** |  | 
**InputCost** | **float32** |  | 
**OutputCost** | **float32** |  | 

## Methods

### NewModelInfo

`func NewModelInfo(provider string, model string, fullName string, baseUrl string, contextLimit int32, maxOutput int32, capabilities NullableString, isDefault bool, inputCost float32, outputCost float32, ) *ModelInfo`

NewModelInfo instantiates a new ModelInfo object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewModelInfoWithDefaults

`func NewModelInfoWithDefaults() *ModelInfo`

NewModelInfoWithDefaults instantiates a new ModelInfo object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetProvider

`func (o *ModelInfo) GetProvider() string`

GetProvider returns the Provider field if non-nil, zero value otherwise.

### GetProviderOk

`func (o *ModelInfo) GetProviderOk() (*string, bool)`

GetProviderOk returns a tuple with the Provider field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetProvider

`func (o *ModelInfo) SetProvider(v string)`

SetProvider sets Provider field to given value.


### GetModel

`func (o *ModelInfo) GetModel() string`

GetModel returns the Model field if non-nil, zero value otherwise.

### GetModelOk

`func (o *ModelInfo) GetModelOk() (*string, bool)`

GetModelOk returns a tuple with the Model field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetModel

`func (o *ModelInfo) SetModel(v string)`

SetModel sets Model field to given value.


### GetFullName

`func (o *ModelInfo) GetFullName() string`

GetFullName returns the FullName field if non-nil, zero value otherwise.

### GetFullNameOk

`func (o *ModelInfo) GetFullNameOk() (*string, bool)`

GetFullNameOk returns a tuple with the FullName field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetFullName

`func (o *ModelInfo) SetFullName(v string)`

SetFullName sets FullName field to given value.


### GetBaseUrl

`func (o *ModelInfo) GetBaseUrl() string`

GetBaseUrl returns the BaseUrl field if non-nil, zero value otherwise.

### GetBaseUrlOk

`func (o *ModelInfo) GetBaseUrlOk() (*string, bool)`

GetBaseUrlOk returns a tuple with the BaseUrl field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetBaseUrl

`func (o *ModelInfo) SetBaseUrl(v string)`

SetBaseUrl sets BaseUrl field to given value.


### GetContextLimit

`func (o *ModelInfo) GetContextLimit() int32`

GetContextLimit returns the ContextLimit field if non-nil, zero value otherwise.

### GetContextLimitOk

`func (o *ModelInfo) GetContextLimitOk() (*int32, bool)`

GetContextLimitOk returns a tuple with the ContextLimit field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetContextLimit

`func (o *ModelInfo) SetContextLimit(v int32)`

SetContextLimit sets ContextLimit field to given value.


### GetMaxOutput

`func (o *ModelInfo) GetMaxOutput() int32`

GetMaxOutput returns the MaxOutput field if non-nil, zero value otherwise.

### GetMaxOutputOk

`func (o *ModelInfo) GetMaxOutputOk() (*int32, bool)`

GetMaxOutputOk returns a tuple with the MaxOutput field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetMaxOutput

`func (o *ModelInfo) SetMaxOutput(v int32)`

SetMaxOutput sets MaxOutput field to given value.


### GetCapabilities

`func (o *ModelInfo) GetCapabilities() string`

GetCapabilities returns the Capabilities field if non-nil, zero value otherwise.

### GetCapabilitiesOk

`func (o *ModelInfo) GetCapabilitiesOk() (*string, bool)`

GetCapabilitiesOk returns a tuple with the Capabilities field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetCapabilities

`func (o *ModelInfo) SetCapabilities(v string)`

SetCapabilities sets Capabilities field to given value.


### SetCapabilitiesNil

`func (o *ModelInfo) SetCapabilitiesNil(b bool)`

 SetCapabilitiesNil sets the value for Capabilities to be an explicit nil

### UnsetCapabilities
`func (o *ModelInfo) UnsetCapabilities()`

UnsetCapabilities ensures that no value is present for Capabilities, not even an explicit nil
### GetIsDefault

`func (o *ModelInfo) GetIsDefault() bool`

GetIsDefault returns the IsDefault field if non-nil, zero value otherwise.

### GetIsDefaultOk

`func (o *ModelInfo) GetIsDefaultOk() (*bool, bool)`

GetIsDefaultOk returns a tuple with the IsDefault field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetIsDefault

`func (o *ModelInfo) SetIsDefault(v bool)`

SetIsDefault sets IsDefault field to given value.


### GetInputCost

`func (o *ModelInfo) GetInputCost() float32`

GetInputCost returns the InputCost field if non-nil, zero value otherwise.

### GetInputCostOk

`func (o *ModelInfo) GetInputCostOk() (*float32, bool)`

GetInputCostOk returns a tuple with the InputCost field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetInputCost

`func (o *ModelInfo) SetInputCost(v float32)`

SetInputCost sets InputCost field to given value.


### GetOutputCost

`func (o *ModelInfo) GetOutputCost() float32`

GetOutputCost returns the OutputCost field if non-nil, zero value otherwise.

### GetOutputCostOk

`func (o *ModelInfo) GetOutputCostOk() (*float32, bool)`

GetOutputCostOk returns a tuple with the OutputCost field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetOutputCost

`func (o *ModelInfo) SetOutputCost(v float32)`

SetOutputCost sets OutputCost field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


