# CreatePlanRequest

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Title** | **string** |  | 
**Descriptionomitempty** | Pointer to **string** |  | [optional] 
**ProjectIdomitempty** | Pointer to **string** |  | [optional] 
**ProjectPathomitempty** | Pointer to **string** |  | [optional] 
**SessionId** | **string** |  | 

## Methods

### NewCreatePlanRequest

`func NewCreatePlanRequest(title string, sessionId string, ) *CreatePlanRequest`

NewCreatePlanRequest instantiates a new CreatePlanRequest object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewCreatePlanRequestWithDefaults

`func NewCreatePlanRequestWithDefaults() *CreatePlanRequest`

NewCreatePlanRequestWithDefaults instantiates a new CreatePlanRequest object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetTitle

`func (o *CreatePlanRequest) GetTitle() string`

GetTitle returns the Title field if non-nil, zero value otherwise.

### GetTitleOk

`func (o *CreatePlanRequest) GetTitleOk() (*string, bool)`

GetTitleOk returns a tuple with the Title field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTitle

`func (o *CreatePlanRequest) SetTitle(v string)`

SetTitle sets Title field to given value.


### GetDescriptionomitempty

`func (o *CreatePlanRequest) GetDescriptionomitempty() string`

GetDescriptionomitempty returns the Descriptionomitempty field if non-nil, zero value otherwise.

### GetDescriptionomitemptyOk

`func (o *CreatePlanRequest) GetDescriptionomitemptyOk() (*string, bool)`

GetDescriptionomitemptyOk returns a tuple with the Descriptionomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetDescriptionomitempty

`func (o *CreatePlanRequest) SetDescriptionomitempty(v string)`

SetDescriptionomitempty sets Descriptionomitempty field to given value.

### HasDescriptionomitempty

`func (o *CreatePlanRequest) HasDescriptionomitempty() bool`

HasDescriptionomitempty returns a boolean if a field has been set.

### GetProjectIdomitempty

`func (o *CreatePlanRequest) GetProjectIdomitempty() string`

GetProjectIdomitempty returns the ProjectIdomitempty field if non-nil, zero value otherwise.

### GetProjectIdomitemptyOk

`func (o *CreatePlanRequest) GetProjectIdomitemptyOk() (*string, bool)`

GetProjectIdomitemptyOk returns a tuple with the ProjectIdomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetProjectIdomitempty

`func (o *CreatePlanRequest) SetProjectIdomitempty(v string)`

SetProjectIdomitempty sets ProjectIdomitempty field to given value.

### HasProjectIdomitempty

`func (o *CreatePlanRequest) HasProjectIdomitempty() bool`

HasProjectIdomitempty returns a boolean if a field has been set.

### GetProjectPathomitempty

`func (o *CreatePlanRequest) GetProjectPathomitempty() string`

GetProjectPathomitempty returns the ProjectPathomitempty field if non-nil, zero value otherwise.

### GetProjectPathomitemptyOk

`func (o *CreatePlanRequest) GetProjectPathomitemptyOk() (*string, bool)`

GetProjectPathomitemptyOk returns a tuple with the ProjectPathomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetProjectPathomitempty

`func (o *CreatePlanRequest) SetProjectPathomitempty(v string)`

SetProjectPathomitempty sets ProjectPathomitempty field to given value.

### HasProjectPathomitempty

`func (o *CreatePlanRequest) HasProjectPathomitempty() bool`

HasProjectPathomitempty returns a boolean if a field has been set.

### GetSessionId

`func (o *CreatePlanRequest) GetSessionId() string`

GetSessionId returns the SessionId field if non-nil, zero value otherwise.

### GetSessionIdOk

`func (o *CreatePlanRequest) GetSessionIdOk() (*string, bool)`

GetSessionIdOk returns a tuple with the SessionId field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSessionId

`func (o *CreatePlanRequest) SetSessionId(v string)`

SetSessionId sets SessionId field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


