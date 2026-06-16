# SkillUIDescriptor

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Slug** | **string** |  | 
**Name** | **string** |  | 
**Description** | **string** |  | 
**UiType** | **string** |  | 
**Categoryomitempty** | Pointer to **string** |  | [optional] 
**Tagsomitempty** | Pointer to **NullableString** |  | [optional] 
**Examplesomitempty** | Pointer to **NullableString** |  | [optional] 
**RiskLevelomitempty** | Pointer to **string** |  | [optional] 
**Bodyomitempty** | Pointer to **string** |  | [optional] 
**Fieldsomitempty** | Pointer to **[]string** |  | [optional] 
**Actionsomitempty** | Pointer to **[]string** |  | [optional] 

## Methods

### NewSkillUIDescriptor

`func NewSkillUIDescriptor(slug string, name string, description string, uiType string, ) *SkillUIDescriptor`

NewSkillUIDescriptor instantiates a new SkillUIDescriptor object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewSkillUIDescriptorWithDefaults

`func NewSkillUIDescriptorWithDefaults() *SkillUIDescriptor`

NewSkillUIDescriptorWithDefaults instantiates a new SkillUIDescriptor object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetSlug

`func (o *SkillUIDescriptor) GetSlug() string`

GetSlug returns the Slug field if non-nil, zero value otherwise.

### GetSlugOk

`func (o *SkillUIDescriptor) GetSlugOk() (*string, bool)`

GetSlugOk returns a tuple with the Slug field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSlug

`func (o *SkillUIDescriptor) SetSlug(v string)`

SetSlug sets Slug field to given value.


### GetName

`func (o *SkillUIDescriptor) GetName() string`

GetName returns the Name field if non-nil, zero value otherwise.

### GetNameOk

`func (o *SkillUIDescriptor) GetNameOk() (*string, bool)`

GetNameOk returns a tuple with the Name field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetName

`func (o *SkillUIDescriptor) SetName(v string)`

SetName sets Name field to given value.


### GetDescription

`func (o *SkillUIDescriptor) GetDescription() string`

GetDescription returns the Description field if non-nil, zero value otherwise.

### GetDescriptionOk

`func (o *SkillUIDescriptor) GetDescriptionOk() (*string, bool)`

GetDescriptionOk returns a tuple with the Description field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetDescription

`func (o *SkillUIDescriptor) SetDescription(v string)`

SetDescription sets Description field to given value.


### GetUiType

`func (o *SkillUIDescriptor) GetUiType() string`

GetUiType returns the UiType field if non-nil, zero value otherwise.

### GetUiTypeOk

`func (o *SkillUIDescriptor) GetUiTypeOk() (*string, bool)`

GetUiTypeOk returns a tuple with the UiType field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetUiType

`func (o *SkillUIDescriptor) SetUiType(v string)`

SetUiType sets UiType field to given value.


### GetCategoryomitempty

`func (o *SkillUIDescriptor) GetCategoryomitempty() string`

GetCategoryomitempty returns the Categoryomitempty field if non-nil, zero value otherwise.

### GetCategoryomitemptyOk

`func (o *SkillUIDescriptor) GetCategoryomitemptyOk() (*string, bool)`

GetCategoryomitemptyOk returns a tuple with the Categoryomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetCategoryomitempty

`func (o *SkillUIDescriptor) SetCategoryomitempty(v string)`

SetCategoryomitempty sets Categoryomitempty field to given value.

### HasCategoryomitempty

`func (o *SkillUIDescriptor) HasCategoryomitempty() bool`

HasCategoryomitempty returns a boolean if a field has been set.

### GetTagsomitempty

`func (o *SkillUIDescriptor) GetTagsomitempty() string`

GetTagsomitempty returns the Tagsomitempty field if non-nil, zero value otherwise.

### GetTagsomitemptyOk

`func (o *SkillUIDescriptor) GetTagsomitemptyOk() (*string, bool)`

GetTagsomitemptyOk returns a tuple with the Tagsomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTagsomitempty

`func (o *SkillUIDescriptor) SetTagsomitempty(v string)`

SetTagsomitempty sets Tagsomitempty field to given value.

### HasTagsomitempty

`func (o *SkillUIDescriptor) HasTagsomitempty() bool`

HasTagsomitempty returns a boolean if a field has been set.

### SetTagsomitemptyNil

`func (o *SkillUIDescriptor) SetTagsomitemptyNil(b bool)`

 SetTagsomitemptyNil sets the value for Tagsomitempty to be an explicit nil

### UnsetTagsomitempty
`func (o *SkillUIDescriptor) UnsetTagsomitempty()`

UnsetTagsomitempty ensures that no value is present for Tagsomitempty, not even an explicit nil
### GetExamplesomitempty

`func (o *SkillUIDescriptor) GetExamplesomitempty() string`

GetExamplesomitempty returns the Examplesomitempty field if non-nil, zero value otherwise.

### GetExamplesomitemptyOk

`func (o *SkillUIDescriptor) GetExamplesomitemptyOk() (*string, bool)`

GetExamplesomitemptyOk returns a tuple with the Examplesomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetExamplesomitempty

`func (o *SkillUIDescriptor) SetExamplesomitempty(v string)`

SetExamplesomitempty sets Examplesomitempty field to given value.

### HasExamplesomitempty

`func (o *SkillUIDescriptor) HasExamplesomitempty() bool`

HasExamplesomitempty returns a boolean if a field has been set.

### SetExamplesomitemptyNil

`func (o *SkillUIDescriptor) SetExamplesomitemptyNil(b bool)`

 SetExamplesomitemptyNil sets the value for Examplesomitempty to be an explicit nil

### UnsetExamplesomitempty
`func (o *SkillUIDescriptor) UnsetExamplesomitempty()`

UnsetExamplesomitempty ensures that no value is present for Examplesomitempty, not even an explicit nil
### GetRiskLevelomitempty

`func (o *SkillUIDescriptor) GetRiskLevelomitempty() string`

GetRiskLevelomitempty returns the RiskLevelomitempty field if non-nil, zero value otherwise.

### GetRiskLevelomitemptyOk

`func (o *SkillUIDescriptor) GetRiskLevelomitemptyOk() (*string, bool)`

GetRiskLevelomitemptyOk returns a tuple with the RiskLevelomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetRiskLevelomitempty

`func (o *SkillUIDescriptor) SetRiskLevelomitempty(v string)`

SetRiskLevelomitempty sets RiskLevelomitempty field to given value.

### HasRiskLevelomitempty

`func (o *SkillUIDescriptor) HasRiskLevelomitempty() bool`

HasRiskLevelomitempty returns a boolean if a field has been set.

### GetBodyomitempty

`func (o *SkillUIDescriptor) GetBodyomitempty() string`

GetBodyomitempty returns the Bodyomitempty field if non-nil, zero value otherwise.

### GetBodyomitemptyOk

`func (o *SkillUIDescriptor) GetBodyomitemptyOk() (*string, bool)`

GetBodyomitemptyOk returns a tuple with the Bodyomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetBodyomitempty

`func (o *SkillUIDescriptor) SetBodyomitempty(v string)`

SetBodyomitempty sets Bodyomitempty field to given value.

### HasBodyomitempty

`func (o *SkillUIDescriptor) HasBodyomitempty() bool`

HasBodyomitempty returns a boolean if a field has been set.

### GetFieldsomitempty

`func (o *SkillUIDescriptor) GetFieldsomitempty() []string`

GetFieldsomitempty returns the Fieldsomitempty field if non-nil, zero value otherwise.

### GetFieldsomitemptyOk

`func (o *SkillUIDescriptor) GetFieldsomitemptyOk() (*[]string, bool)`

GetFieldsomitemptyOk returns a tuple with the Fieldsomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetFieldsomitempty

`func (o *SkillUIDescriptor) SetFieldsomitempty(v []string)`

SetFieldsomitempty sets Fieldsomitempty field to given value.

### HasFieldsomitempty

`func (o *SkillUIDescriptor) HasFieldsomitempty() bool`

HasFieldsomitempty returns a boolean if a field has been set.

### SetFieldsomitemptyNil

`func (o *SkillUIDescriptor) SetFieldsomitemptyNil(b bool)`

 SetFieldsomitemptyNil sets the value for Fieldsomitempty to be an explicit nil

### UnsetFieldsomitempty
`func (o *SkillUIDescriptor) UnsetFieldsomitempty()`

UnsetFieldsomitempty ensures that no value is present for Fieldsomitempty, not even an explicit nil
### GetActionsomitempty

`func (o *SkillUIDescriptor) GetActionsomitempty() []string`

GetActionsomitempty returns the Actionsomitempty field if non-nil, zero value otherwise.

### GetActionsomitemptyOk

`func (o *SkillUIDescriptor) GetActionsomitemptyOk() (*[]string, bool)`

GetActionsomitemptyOk returns a tuple with the Actionsomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetActionsomitempty

`func (o *SkillUIDescriptor) SetActionsomitempty(v []string)`

SetActionsomitempty sets Actionsomitempty field to given value.

### HasActionsomitempty

`func (o *SkillUIDescriptor) HasActionsomitempty() bool`

HasActionsomitempty returns a boolean if a field has been set.

### SetActionsomitemptyNil

`func (o *SkillUIDescriptor) SetActionsomitemptyNil(b bool)`

 SetActionsomitemptyNil sets the value for Actionsomitempty to be an explicit nil

### UnsetActionsomitempty
`func (o *SkillUIDescriptor) UnsetActionsomitempty()`

UnsetActionsomitempty ensures that no value is present for Actionsomitempty, not even an explicit nil

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


