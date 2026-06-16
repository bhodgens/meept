# TemplateInfo

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Name** | **string** |  | 
**Description** | **string** |  | 
**Scope** | **map[string]interface{}** |  | 
**Pathomitempty** | Pointer to **string** |  | [optional] 
**Priority** | **int32** |  | 
**Bodyomitempty** | Pointer to **string** |  | [optional] 

## Methods

### NewTemplateInfo

`func NewTemplateInfo(name string, description string, scope map[string]interface{}, priority int32, ) *TemplateInfo`

NewTemplateInfo instantiates a new TemplateInfo object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewTemplateInfoWithDefaults

`func NewTemplateInfoWithDefaults() *TemplateInfo`

NewTemplateInfoWithDefaults instantiates a new TemplateInfo object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetName

`func (o *TemplateInfo) GetName() string`

GetName returns the Name field if non-nil, zero value otherwise.

### GetNameOk

`func (o *TemplateInfo) GetNameOk() (*string, bool)`

GetNameOk returns a tuple with the Name field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetName

`func (o *TemplateInfo) SetName(v string)`

SetName sets Name field to given value.


### GetDescription

`func (o *TemplateInfo) GetDescription() string`

GetDescription returns the Description field if non-nil, zero value otherwise.

### GetDescriptionOk

`func (o *TemplateInfo) GetDescriptionOk() (*string, bool)`

GetDescriptionOk returns a tuple with the Description field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetDescription

`func (o *TemplateInfo) SetDescription(v string)`

SetDescription sets Description field to given value.


### GetScope

`func (o *TemplateInfo) GetScope() map[string]interface{}`

GetScope returns the Scope field if non-nil, zero value otherwise.

### GetScopeOk

`func (o *TemplateInfo) GetScopeOk() (*map[string]interface{}, bool)`

GetScopeOk returns a tuple with the Scope field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetScope

`func (o *TemplateInfo) SetScope(v map[string]interface{})`

SetScope sets Scope field to given value.


### GetPathomitempty

`func (o *TemplateInfo) GetPathomitempty() string`

GetPathomitempty returns the Pathomitempty field if non-nil, zero value otherwise.

### GetPathomitemptyOk

`func (o *TemplateInfo) GetPathomitemptyOk() (*string, bool)`

GetPathomitemptyOk returns a tuple with the Pathomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPathomitempty

`func (o *TemplateInfo) SetPathomitempty(v string)`

SetPathomitempty sets Pathomitempty field to given value.

### HasPathomitempty

`func (o *TemplateInfo) HasPathomitempty() bool`

HasPathomitempty returns a boolean if a field has been set.

### GetPriority

`func (o *TemplateInfo) GetPriority() int32`

GetPriority returns the Priority field if non-nil, zero value otherwise.

### GetPriorityOk

`func (o *TemplateInfo) GetPriorityOk() (*int32, bool)`

GetPriorityOk returns a tuple with the Priority field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPriority

`func (o *TemplateInfo) SetPriority(v int32)`

SetPriority sets Priority field to given value.


### GetBodyomitempty

`func (o *TemplateInfo) GetBodyomitempty() string`

GetBodyomitempty returns the Bodyomitempty field if non-nil, zero value otherwise.

### GetBodyomitemptyOk

`func (o *TemplateInfo) GetBodyomitemptyOk() (*string, bool)`

GetBodyomitemptyOk returns a tuple with the Bodyomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetBodyomitempty

`func (o *TemplateInfo) SetBodyomitempty(v string)`

SetBodyomitempty sets Bodyomitempty field to given value.

### HasBodyomitempty

`func (o *TemplateInfo) HasBodyomitempty() bool`

HasBodyomitempty returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


