"""Fixed workload and accounting shared by the owned W11 experiment."""
import re


def metric_delta(before, after):
    def parse(raw):
        values = {}
        for line in raw.splitlines():
            parts = line.split()
            if len(parts) == 2 and not line.startswith('#'):
                values[parts[0]] = float(parts[1])
        return values
    a, b = parse(before), parse(after)
    result = {key: b[key] - value for key, value in a.items()
              if key in b and key.endswith('_total')}
    if any(value < 0 for value in result.values()):
        raise ValueError('process counters reset during measurement')
    return result


def reset_sql(project):
    if not re.fullmatch(r'nexusdesk-week11-[a-f0-9]{12}', project):
        raise ValueError('requires owned W11 project')
    return '''DELETE FROM ticket_events WHERE ticket_id>20000;
DELETE FROM tickets WHERE id>20000;
ALTER TABLE tickets ALTER COLUMN id RESTART WITH 20001;
ALTER TABLE ticket_events ALTER COLUMN id RESTART WITH 1;
UPDATE cache_revisions SET version=10000;
VACUUM ANALYZE tickets;
VACUUM ANALYZE ticket_events;'''


def workload(org):
    base = f'/api/v1/organizations/{int(org)}'
    return {
        'ticket_list': {'method': 'GET', 'path': base + '/tickets?page=1&page_size=100'},
        'knowledge_list': {'method': 'GET', 'path': base + '/knowledge/documents?page=1&page_size=100'},
        'ticket_detail': {'method': 'GET', 'path': base + '/tickets/1'},
        'ticket_create': {'method': 'POST', 'path': base + '/tickets',
                          'body': '{"title":"W11 controlled create","body":"Synthetic bounded create body.","priority":2}'},
    }


def frozen_overlay(root, manifest, snapshot, cache_path='internal/platform/cache/cache.go'):
    """Rebuild old decoder without accepting changed product source as baseline."""
    import hashlib
    if (not snapshot.is_file() or cache_path not in manifest
            or hashlib.sha256(snapshot.read_bytes()).hexdigest() != manifest[cache_path]):
        raise ValueError('frozen baseline snapshot missing or hash mismatch; restore the retained W11 evidence')
    for name, digest in manifest.items():
        path = root / name
        if name != cache_path and (not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != digest):
            raise ValueError('product source differs from frozen baseline: ' + name)
    return {'Replace': {str(root / cache_path): str(snapshot.resolve())}}
