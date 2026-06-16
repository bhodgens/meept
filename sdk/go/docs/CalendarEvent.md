# CalendarEvent

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Id** | **string** |  | 
**Summary** | **string** |  | 
**Descriptionomitempty** | Pointer to **string** |  | [optional] 
**Locationomitempty** | Pointer to **string** |  | [optional] 
**Start** | **string** |  | 
**End** | **string** |  | 
**AllDay** | **bool** |  | 
**Statusomitempty** | Pointer to **string** |  | [optional] 
**HtmlLinkomitempty** | Pointer to **string** |  | [optional] 
**Attendeesomitempty** | Pointer to **[]string** |  | [optional] 

## Methods

### NewCalendarEvent

`func NewCalendarEvent(id string, summary string, start string, end string, allDay bool, ) *CalendarEvent`

NewCalendarEvent instantiates a new CalendarEvent object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewCalendarEventWithDefaults

`func NewCalendarEventWithDefaults() *CalendarEvent`

NewCalendarEventWithDefaults instantiates a new CalendarEvent object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetId

`func (o *CalendarEvent) GetId() string`

GetId returns the Id field if non-nil, zero value otherwise.

### GetIdOk

`func (o *CalendarEvent) GetIdOk() (*string, bool)`

GetIdOk returns a tuple with the Id field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetId

`func (o *CalendarEvent) SetId(v string)`

SetId sets Id field to given value.


### GetSummary

`func (o *CalendarEvent) GetSummary() string`

GetSummary returns the Summary field if non-nil, zero value otherwise.

### GetSummaryOk

`func (o *CalendarEvent) GetSummaryOk() (*string, bool)`

GetSummaryOk returns a tuple with the Summary field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSummary

`func (o *CalendarEvent) SetSummary(v string)`

SetSummary sets Summary field to given value.


### GetDescriptionomitempty

`func (o *CalendarEvent) GetDescriptionomitempty() string`

GetDescriptionomitempty returns the Descriptionomitempty field if non-nil, zero value otherwise.

### GetDescriptionomitemptyOk

`func (o *CalendarEvent) GetDescriptionomitemptyOk() (*string, bool)`

GetDescriptionomitemptyOk returns a tuple with the Descriptionomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetDescriptionomitempty

`func (o *CalendarEvent) SetDescriptionomitempty(v string)`

SetDescriptionomitempty sets Descriptionomitempty field to given value.

### HasDescriptionomitempty

`func (o *CalendarEvent) HasDescriptionomitempty() bool`

HasDescriptionomitempty returns a boolean if a field has been set.

### GetLocationomitempty

`func (o *CalendarEvent) GetLocationomitempty() string`

GetLocationomitempty returns the Locationomitempty field if non-nil, zero value otherwise.

### GetLocationomitemptyOk

`func (o *CalendarEvent) GetLocationomitemptyOk() (*string, bool)`

GetLocationomitemptyOk returns a tuple with the Locationomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetLocationomitempty

`func (o *CalendarEvent) SetLocationomitempty(v string)`

SetLocationomitempty sets Locationomitempty field to given value.

### HasLocationomitempty

`func (o *CalendarEvent) HasLocationomitempty() bool`

HasLocationomitempty returns a boolean if a field has been set.

### GetStart

`func (o *CalendarEvent) GetStart() string`

GetStart returns the Start field if non-nil, zero value otherwise.

### GetStartOk

`func (o *CalendarEvent) GetStartOk() (*string, bool)`

GetStartOk returns a tuple with the Start field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetStart

`func (o *CalendarEvent) SetStart(v string)`

SetStart sets Start field to given value.


### GetEnd

`func (o *CalendarEvent) GetEnd() string`

GetEnd returns the End field if non-nil, zero value otherwise.

### GetEndOk

`func (o *CalendarEvent) GetEndOk() (*string, bool)`

GetEndOk returns a tuple with the End field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetEnd

`func (o *CalendarEvent) SetEnd(v string)`

SetEnd sets End field to given value.


### GetAllDay

`func (o *CalendarEvent) GetAllDay() bool`

GetAllDay returns the AllDay field if non-nil, zero value otherwise.

### GetAllDayOk

`func (o *CalendarEvent) GetAllDayOk() (*bool, bool)`

GetAllDayOk returns a tuple with the AllDay field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetAllDay

`func (o *CalendarEvent) SetAllDay(v bool)`

SetAllDay sets AllDay field to given value.


### GetStatusomitempty

`func (o *CalendarEvent) GetStatusomitempty() string`

GetStatusomitempty returns the Statusomitempty field if non-nil, zero value otherwise.

### GetStatusomitemptyOk

`func (o *CalendarEvent) GetStatusomitemptyOk() (*string, bool)`

GetStatusomitemptyOk returns a tuple with the Statusomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetStatusomitempty

`func (o *CalendarEvent) SetStatusomitempty(v string)`

SetStatusomitempty sets Statusomitempty field to given value.

### HasStatusomitempty

`func (o *CalendarEvent) HasStatusomitempty() bool`

HasStatusomitempty returns a boolean if a field has been set.

### GetHtmlLinkomitempty

`func (o *CalendarEvent) GetHtmlLinkomitempty() string`

GetHtmlLinkomitempty returns the HtmlLinkomitempty field if non-nil, zero value otherwise.

### GetHtmlLinkomitemptyOk

`func (o *CalendarEvent) GetHtmlLinkomitemptyOk() (*string, bool)`

GetHtmlLinkomitemptyOk returns a tuple with the HtmlLinkomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetHtmlLinkomitempty

`func (o *CalendarEvent) SetHtmlLinkomitempty(v string)`

SetHtmlLinkomitempty sets HtmlLinkomitempty field to given value.

### HasHtmlLinkomitempty

`func (o *CalendarEvent) HasHtmlLinkomitempty() bool`

HasHtmlLinkomitempty returns a boolean if a field has been set.

### GetAttendeesomitempty

`func (o *CalendarEvent) GetAttendeesomitempty() []string`

GetAttendeesomitempty returns the Attendeesomitempty field if non-nil, zero value otherwise.

### GetAttendeesomitemptyOk

`func (o *CalendarEvent) GetAttendeesomitemptyOk() (*[]string, bool)`

GetAttendeesomitemptyOk returns a tuple with the Attendeesomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetAttendeesomitempty

`func (o *CalendarEvent) SetAttendeesomitempty(v []string)`

SetAttendeesomitempty sets Attendeesomitempty field to given value.

### HasAttendeesomitempty

`func (o *CalendarEvent) HasAttendeesomitempty() bool`

HasAttendeesomitempty returns a boolean if a field has been set.

### SetAttendeesomitemptyNil

`func (o *CalendarEvent) SetAttendeesomitemptyNil(b bool)`

 SetAttendeesomitemptyNil sets the value for Attendeesomitempty to be an explicit nil

### UnsetAttendeesomitempty
`func (o *CalendarEvent) UnsetAttendeesomitempty()`

UnsetAttendeesomitempty ensures that no value is present for Attendeesomitempty, not even an explicit nil

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


