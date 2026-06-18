//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_collection/built_collection.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'calendar_event.g.dart';

/// CalendarEvent
///
/// Properties:
/// * [id] 
/// * [summary] 
/// * [descriptionCommaOmitempty] 
/// * [locationCommaOmitempty] 
/// * [start] 
/// * [end] 
/// * [allDay] 
/// * [statusCommaOmitempty] 
/// * [htmlLinkCommaOmitempty] 
/// * [attendeesCommaOmitempty] 
@BuiltValue()
abstract class CalendarEvent implements Built<CalendarEvent, CalendarEventBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'summary')
  String get summary;

  @BuiltValueField(wireName: r'description,omitempty')
  String? get descriptionCommaOmitempty;

  @BuiltValueField(wireName: r'location,omitempty')
  String? get locationCommaOmitempty;

  @BuiltValueField(wireName: r'start')
  String get start;

  @BuiltValueField(wireName: r'end')
  String get end;

  @BuiltValueField(wireName: r'all_day')
  bool get allDay;

  @BuiltValueField(wireName: r'status,omitempty')
  String? get statusCommaOmitempty;

  @BuiltValueField(wireName: r'html_link,omitempty')
  String? get htmlLinkCommaOmitempty;

  @BuiltValueField(wireName: r'attendees,omitempty')
  BuiltList<String>? get attendeesCommaOmitempty;

  CalendarEvent._();

  factory CalendarEvent([void updates(CalendarEventBuilder b)]) = _$CalendarEvent;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(CalendarEventBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<CalendarEvent> get serializer => _$CalendarEventSerializer();
}

class _$CalendarEventSerializer implements PrimitiveSerializer<CalendarEvent> {
  @override
  final Iterable<Type> types = const [CalendarEvent, _$CalendarEvent];

  @override
  final String wireName = r'CalendarEvent';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    CalendarEvent object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'id';
    yield serializers.serialize(
      object.id,
      specifiedType: const FullType(String),
    );
    yield r'summary';
    yield serializers.serialize(
      object.summary,
      specifiedType: const FullType(String),
    );
    if (object.descriptionCommaOmitempty != null) {
      yield r'description,omitempty';
      yield serializers.serialize(
        object.descriptionCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.locationCommaOmitempty != null) {
      yield r'location,omitempty';
      yield serializers.serialize(
        object.locationCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    yield r'start';
    yield serializers.serialize(
      object.start,
      specifiedType: const FullType(String),
    );
    yield r'end';
    yield serializers.serialize(
      object.end,
      specifiedType: const FullType(String),
    );
    yield r'all_day';
    yield serializers.serialize(
      object.allDay,
      specifiedType: const FullType(bool),
    );
    if (object.statusCommaOmitempty != null) {
      yield r'status,omitempty';
      yield serializers.serialize(
        object.statusCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.htmlLinkCommaOmitempty != null) {
      yield r'html_link,omitempty';
      yield serializers.serialize(
        object.htmlLinkCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.attendeesCommaOmitempty != null) {
      yield r'attendees,omitempty';
      yield serializers.serialize(
        object.attendeesCommaOmitempty,
        specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    CalendarEvent object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required CalendarEventBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.id = valueDes;
          break;
        case r'summary':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.summary = valueDes;
          break;
        case r'description,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.descriptionCommaOmitempty = valueDes;
          break;
        case r'location,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.locationCommaOmitempty = valueDes;
          break;
        case r'start':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.start = valueDes;
          break;
        case r'end':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.end = valueDes;
          break;
        case r'all_day':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.allDay = valueDes;
          break;
        case r'status,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.statusCommaOmitempty = valueDes;
          break;
        case r'html_link,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.htmlLinkCommaOmitempty = valueDes;
          break;
        case r'attendees,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
          ) as BuiltList<String>?;
          if (valueDes == null) continue;
          result.attendeesCommaOmitempty.replace(valueDes);
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  CalendarEvent deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = CalendarEventBuilder();
    final serializedList = (serialized as Iterable<Object?>).toList();
    final unhandled = <Object?>[];
    _deserializeProperties(
      serializers,
      serialized,
      specifiedType: specifiedType,
      serializedList: serializedList,
      unhandled: unhandled,
      result: result,
    );
    return result.build();
  }
}

