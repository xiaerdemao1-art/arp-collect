# Useful ARP Queries

The `(device_name, ip, mac)` primary key in `arp_entry_current` makes several
operational queries straightforward. `created_at` records when a triple was
first seen; `collected_at` records the last time it was observed.

## 1. IP conflict detection

Same device, same IP, multiple MACs - a sign of IP misconfiguration or a
rogue device:

```sql
SELECT device_name, ip, COUNT(DISTINCT mac) AS mac_count,
       STRING_AGG(DISTINCT mac, ', ') AS macs
FROM arp_entry_current
GROUP BY device_name, ip
HAVING COUNT(DISTINCT mac) > 1
ORDER BY mac_count DESC;
```

## 2. Endpoint drift tracking

Trace where a single MAC has appeared over time:

```sql
SELECT device_name, ip, interface,
       created_at, collected_at,
       EXTRACT(EPOCH FROM (collected_at - created_at)) / 3600 AS hours_alive
FROM arp_entry_current
WHERE mac = 'aa:bb:cc:dd:ee:ff'
ORDER BY collected_at DESC;
```

## 3. Multi-gateway visibility (stacking / VRRP)

Endpoints seen by multiple gateways simultaneously:

```sql
SELECT ip, mac, COUNT(DISTINCT device_name) AS gateway_count,
       STRING_AGG(DISTINCT device_name, ' | ') AS gateways
FROM arp_entry_current
GROUP BY ip, mac
HAVING COUNT(DISTINCT device_name) > 1
ORDER BY gateway_count DESC
LIMIT 20;
```

## 4. NIC replacement history

All MACs ever seen for a given device + IP:

```sql
SELECT mac, interface, created_at, collected_at,
       CASE
         WHEN collected_at > NOW() - INTERVAL '1 hour' THEN 'active'
         ELSE 'offline'
       END AS status
FROM arp_entry_current
WHERE device_name = 'your-device-name' AND ip = '192.0.2.100'
ORDER BY collected_at DESC;
```

## 5. Stale-entry cleanup

Remove entries not seen in the last 30 days:

```sql
DELETE FROM arp_entry_current
WHERE collected_at < NOW() - INTERVAL '30 days';
```
