//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'create_event_request.g.dart';

/// CreateEventRequest
///
/// Properties:
/// * [summary] 
/// * [description] 
/// * [location] 
/// * [start] 
/// * [end] 
/// * [attendees] 
@BuiltValue()
abstract class CreateEventRequest implements Built<CreateEventRequest, CreateEventRequestBuilder> {
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

  @BuiltValueField(wireName: r'attendees')
  String? get attendees;

  CreateEventRequest._();

  factory CreateEventRequest([void updates(CreateEventRequestBuilder b)]) = _$CreateEventRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(CreateEventRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<CreateEventRequest> get serializer => _$CreateEventRequestSerializer();
}

class _$CreateEventRequestSerializer implements PrimitiveSerializer<CreateEventRequest> {
  @override
  final Iterable<Type> types = const [CreateEventRequest, _$CreateEventRequest];

  @override
  final String wireName = r'CreateEventRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    CreateEventRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
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
    if (object.attendees != null) {
      yield r'attendees';
      yield serializers.serialize(
        object.attendees,
        specifiedType: const FullType.nullable(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    CreateEventRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required CreateEventRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
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
        case r'attendees':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.attendees = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  CreateEventRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = CreateEventRequestBuilder();
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

