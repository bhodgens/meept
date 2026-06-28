import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/prompts/prompt_models.dart';

void main() {
  group('PromptSummary.fromJson', () {
    test('parses all fields', () {
      final s = PromptSummary.fromJson(const {
        'name': 'planner/decompose.md',
        'tier': 'USER', // case-insensitive
        'source_path': '/home/u/.meept/prompts/planner/decompose.md',
        'modified': '2026-06-27T10:30:00Z',
      });
      expect(s.name, 'planner/decompose.md');
      expect(s.tier, 'user');
      expect(s.isUserTier, isTrue);
      expect(s.isProjectTier, isFalse);
      expect(s.sourcePath, '/home/u/.meept/prompts/planner/decompose.md');
      expect(s.modified, isNotNull);
      expect(s.modified!.toUtc().toIso8601String(),
          '2026-06-27T10:30:00.000Z');
    });

    test('parses project tier', () {
      final s = PromptSummary.fromJson(const {
        'name': 'x.md',
        'tier': 'project',
        'source_path': '',
      });
      expect(s.tier, 'project');
      expect(s.isProjectTier, isTrue);
      expect(s.isUserTier, isFalse);
      expect(s.modified, isNull);
    });

    test('parses bundled tier', () {
      final s = PromptSummary.fromJson(const {
        'name': 'planner/interview.md',
        'tier': 'bundled',
        'source_path': 'config/prompts/planner/interview.md',
        'modified': '2026-01-01T00:00:00Z',
      });
      expect(s.tier, 'bundled');
      expect(s.isUserTier, isFalse);
    });

    test('parses system tier', () {
      final s = PromptSummary.fromJson(const {
        'name': 'sys.md',
        'tier': 'system',
        'source_path': '/etc/meept/prompts/sys.md',
      });
      expect(s.tier, 'system');
      expect(s.isUserTier, isFalse);
    });

    test('missing fields default to empty', () {
      final s = PromptSummary.fromJson(const {});
      expect(s.name, '');
      expect(s.tier, '');
      expect(s.sourcePath, '');
      expect(s.modified, isNull);
      expect(s.isUserTier, isFalse);
    });

    test('malformed modified leaves modified null', () {
      final s = PromptSummary.fromJson(const {
        'name': 'x.md',
        'tier': 'bundled',
        'source_path': '',
        'modified': 'not a date',
      });
      expect(s.modified, isNull);
    });

    test('numeric modified is ignored', () {
      final s = PromptSummary.fromJson(const {
        'name': 'x.md',
        'tier': 'bundled',
        'modified': 12345,
      });
      expect(s.modified, isNull);
    });

    test('empty tier is treated as not-user and not-project', () {
      final s = PromptSummary.fromJson(const {'name': 'a', 'tier': ''});
      expect(s.isUserTier, isFalse);
      expect(s.isProjectTier, isFalse);
    });
  });

  group('PromptDetail.fromJson', () {
    test('parses content + metadata', () {
      final d = PromptDetail.fromJson(const {
        'name': 'planner/decompose.md',
        'tier': 'user',
        'source_path': '/h/.meept/prompts/planner/decompose.md',
        'modified': '2026-06-27T10:30:00Z',
        'content': 'hello {{.Input}}',
      });
      expect(d.name, 'planner/decompose.md');
      expect(d.tier, 'user');
      expect(d.sourcePath, '/h/.meept/prompts/planner/decompose.md');
      expect(d.content, 'hello {{.Input}}');
      expect(d.modified, isNotNull);
    });

    test('missing content defaults to empty string', () {
      final d = PromptDetail.fromJson(const {
        'name': 'x.md',
        'tier': 'bundled',
        'source_path': '',
      });
      expect(d.content, '');
    });

    test('tier is lowercased', () {
      final d = PromptDetail.fromJson(const {
        'name': 'x.md',
        'tier': 'PROJECT',
        'source_path': '',
        'content': '',
      });
      expect(d.tier, 'project');
    });
  });

  group('PromptValidateRequest', () {
    test('empty name omits the field', () {
      const req = PromptValidateRequest();
      expect(req.toJson(), isEmpty);
    });

    test('null name omits the field', () {
      const req = PromptValidateRequest(name: null);
      expect(req.toJson(), isEmpty);
    });

    test('non-empty name is included', () {
      const req = PromptValidateRequest(name: 'planner/x.md');
      expect(req.toJson(), {'name': 'planner/x.md'});
    });

    test('empty string name is treated like null', () {
      const req = PromptValidateRequest(name: '');
      expect(req.toJson(), isEmpty);
    });
  });

  group('PromptValidateResult.fromJson', () {
    test('single-template valid response', () {
      final r = PromptValidateResult.fromJson(const {
        'name': 'planner/decompose.md',
        'valid': true,
      });
      expect(r.name, 'planner/decompose.md');
      expect(r.valid, isTrue);
      expect(r.error, '');
      expect(r.errors, isEmpty);
      expect(r.checked, 0);
    });

    test('single-template invalid response carries error', () {
      final r = PromptValidateResult.fromJson(const {
        'name': 'planner/broken.md',
        'valid': false,
        'error': 'template: planner/broken.md:5: unexpected "}"',
      });
      expect(r.name, 'planner/broken.md');
      expect(r.valid, isFalse);
      expect(r.error, 'template: planner/broken.md:5: unexpected "}"');
    });

    test('bulk-mode all-valid response', () {
      final r = PromptValidateResult.fromJson(const {
        'valid': true,
        'errors': <Map<String, dynamic>>[],
        'checked': 12,
      });
      expect(r.name, '');
      expect(r.valid, isTrue);
      expect(r.errors, isEmpty);
      expect(r.checked, 12);
    });

    test('bulk-mode with errors', () {
      final r = PromptValidateResult.fromJson(const {
        'valid': false,
        'errors': <Map<String, dynamic>>[
          {'name': 'a.md', 'error': 'boom'},
          {'name': 'b.md', 'error': 'bang'},
        ],
        'checked': 2,
      });
      expect(r.valid, isFalse);
      expect(r.errors.length, 2);
      expect(r.errors.first.name, 'a.md');
      expect(r.errors.first.error, 'boom');
      expect(r.errors.last.name, 'b.md');
      expect(r.errors.last.error, 'bang');
      expect(r.checked, 2);
    });

    test('missing valid field defaults to false', () {
      final r = PromptValidateResult.fromJson(const {});
      expect(r.valid, isFalse);
    });

    test('non-numeric checked coerces to 0', () {
      final r = PromptValidateResult.fromJson(const {
        'valid': true,
        'checked': 'oops',
      });
      expect(r.checked, 0);
    });
  });

  group('PromptValidateError.fromJson', () {
    test('parses both fields', () {
      final e = PromptValidateError.fromJson(const {
        'name': 'foo.md',
        'error': 'template parse error',
      });
      expect(e.name, 'foo.md');
      expect(e.error, 'template parse error');
    });

    test('missing fields default to empty', () {
      final e = PromptValidateError.fromJson(const {});
      expect(e.name, '');
      expect(e.error, '');
    });
  });
}
