# UIFieldDef

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Name** | **string** |  | 
**Label** | **string** |  | 
**Type** | **string** |  | 
**Requiredomitempty** | Pointer to **bool** |  | [optional] 
**Defaultomitempty** | Pointer to **map[string]interface{}** |  | [optional] 
**Optionsomitempty** | Pointer to **NullableString** |  | [optional] 
**Placeholderomitempty** | Pointer to **string** |  | [optional] 
**Helpomitempty** | Pointer to **string** |  | [optional] 

## Methods

### NewUIFieldDef

`func NewUIFieldDef(name string, label string, type_ string, ) *UIFieldDef`

NewUIFieldDef instantiates a new UIFieldDef object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewUIFieldDefWithDefaults

`func NewUIFieldDefWithDefaults() *UIFieldDef`

NewUIFieldDefWithDefaults instantiates a new UIFieldDef object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetName

`func (o *UIFieldDef) GetName() string`

GetName returns the Name field if non-nil, zero value otherwise.

### GetNameOk

`func (o *UIFieldDef) GetNameOk() (*string, bool)`

GetNameOk returns a tuple with the Name field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetName

`func (o *UIFieldDef) SetName(v string)`

SetName sets Name field to given value.


### GetLabel

`func (o *UIFieldDef) GetLabel() string`

GetLabel returns the Label field if non-nil, zero value otherwise.

### GetLabelOk

`func (o *UIFieldDef) GetLabelOk() (*string, bool)`

GetLabelOk returns a tuple with the Label field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetLabel

`func (o *UIFieldDef) SetLabel(v string)`

SetLabel sets Label field to given value.


### GetType

`func (o *UIFieldDef) GetType() string`

GetType returns the Type field if non-nil, zero value otherwise.

### GetTypeOk

`func (o *UIFieldDef) GetTypeOk() (*string, bool)`

GetTypeOk returns a tuple with the Type field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetType

`func (o *UIFieldDef) SetType(v string)`

SetType sets Type field to given value.


### GetRequiredomitempty

`func (o *UIFieldDef) GetRequiredomitempty() bool`

GetRequiredomitempty returns the Requiredomitempty field if non-nil, zero value otherwise.

### GetRequiredomitemptyOk

`func (o *UIFieldDef) GetRequiredomitemptyOk() (*bool, bool)`

GetRequiredomitemptyOk returns a tuple with the Requiredomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetRequiredomitempty

`func (o *UIFieldDef) SetRequiredomitempty(v bool)`

SetRequiredomitempty sets Requiredomitempty field to given value.

### HasRequiredomitempty

`func (o *UIFieldDef) HasRequiredomitempty() bool`

HasRequiredomitempty returns a boolean if a field has been set.

### GetDefaultomitempty

`func (o *UIFieldDef) GetDefaultomitempty() map[string]interface{}`

GetDefaultomitempty returns the Defaultomitempty field if non-nil, zero value otherwise.

### GetDefaultomitemptyOk

`func (o *UIFieldDef) GetDefaultomitemptyOk() (*map[string]interface{}, bool)`

GetDefaultomitemptyOk returns a tuple with the Defaultomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetDefaultomitempty

`func (o *UIFieldDef) SetDefaultomitempty(v map[string]interface{})`

SetDefaultomitempty sets Defaultomitempty field to given value.

### HasDefaultomitempty

`func (o *UIFieldDef) HasDefaultomitempty() bool`

HasDefaultomitempty returns a boolean if a field has been set.

### GetOptionsomitempty

`func (o *UIFieldDef) GetOptionsomitempty() string`

GetOptionsomitempty returns the Optionsomitempty field if non-nil, zero value otherwise.

### GetOptionsomitemptyOk

`func (o *UIFieldDef) GetOptionsomitemptyOk() (*string, bool)`

GetOptionsomitemptyOk returns a tuple with the Optionsomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetOptionsomitempty

`func (o *UIFieldDef) SetOptionsomitempty(v string)`

SetOptionsomitempty sets Optionsomitempty field to given value.

### HasOptionsomitempty

`func (o *UIFieldDef) HasOptionsomitempty() bool`

HasOptionsomitempty returns a boolean if a field has been set.

### SetOptionsomitemptyNil

`func (o *UIFieldDef) SetOptionsomitemptyNil(b bool)`

 SetOptionsomitemptyNil sets the value for Optionsomitempty to be an explicit nil

### UnsetOptionsomitempty
`func (o *UIFieldDef) UnsetOptionsomitempty()`

UnsetOptionsomitempty ensures that no value is present for Optionsomitempty, not even an explicit nil
### GetPlaceholderomitempty

`func (o *UIFieldDef) GetPlaceholderomitempty() string`

GetPlaceholderomitempty returns the Placeholderomitempty field if non-nil, zero value otherwise.

### GetPlaceholderomitemptyOk

`func (o *UIFieldDef) GetPlaceholderomitemptyOk() (*string, bool)`

GetPlaceholderomitemptyOk returns a tuple with the Placeholderomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPlaceholderomitempty

`func (o *UIFieldDef) SetPlaceholderomitempty(v string)`

SetPlaceholderomitempty sets Placeholderomitempty field to given value.

### HasPlaceholderomitempty

`func (o *UIFieldDef) HasPlaceholderomitempty() bool`

HasPlaceholderomitempty returns a boolean if a field has been set.

### GetHelpomitempty

`func (o *UIFieldDef) GetHelpomitempty() string`

GetHelpomitempty returns the Helpomitempty field if non-nil, zero value otherwise.

### GetHelpomitemptyOk

`func (o *UIFieldDef) GetHelpomitemptyOk() (*string, bool)`

GetHelpomitemptyOk returns a tuple with the Helpomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetHelpomitempty

`func (o *UIFieldDef) SetHelpomitempty(v string)`

SetHelpomitempty sets Helpomitempty field to given value.

### HasHelpomitempty

`func (o *UIFieldDef) HasHelpomitempty() bool`

HasHelpomitempty returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


