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
/// * [description] 
/// * [location] 
/// * [start] 
/// * [end] 
/// * [allDay] 
/// * [status] 
/// * [htmlLink] 
/// * [attendees] 
@BuiltValue()
abstract class CalendarEvent implements Built<CalendarEvent, CalendarEventBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'summary')
  String get summary;

  @BuiltValueField(wireName: r'description')
  String? get description;

  @BuiltValueField(wireName: r'location')
  String? get location;

  @BuiltValueField(wireName: r'start')
  String get start;

  @BuiltValueField(wireName: r'end')
  String get end;

  @BuiltValueField(wireName: r'all_day')
  bool get allDay;

  @BuiltValueField(wireName: r'status')
  String? get status;

  @BuiltValueField(wireName: r'html_link')
  String? get htmlLink;

  @BuiltValueField(wireName: r'attendees')
  BuiltList<String>? get attendees;

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
    if (object.description != null) {
      yield r'description';
      yield serializers.serialize(
        object.description,
        specifiedType: const FullType(String),
      );
    }
    if (object.location != null) {
      yield r'location';
      yield serializers.serialize(
        object.location,
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
    if (object.status != null) {
      yield r'status';
      yield serializers.serialize(
        object.status,
        specifiedType: const FullType(String),
      );
    }
    if (object.htmlLink != null) {
      yield r'html_link';
      yield serializers.serialize(
        object.htmlLink,
        specifiedType: const FullType(String),
      );
    }
    if (object.attendees != null) {
      yield r'attendees';
      yield serializers.serialize(
        object.attendees,
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
        case r'description':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.description = valueDes;
          break;
        case r'location':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.location = valueDes;
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
        case r'status':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.status = valueDes;
          break;
        case r'html_link':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.htmlLink = valueDes;
          break;
        case r'attendees':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
          ) as BuiltList<String>?;
          if (valueDes == null) continue;
          result.attendees.replace(valueDes);
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

