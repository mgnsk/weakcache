## Weak cache

Weak cache implementation in go. It tracks the reference count of all cached records and evicts one when all references to it have been unreachable for a specified duration of time. A "reference" is simply a unique pointer to the record value which is returned to the caller.

Before returning the pointer, the reference count for the record is increased. Each unique pointer has a finalizer that will decrease the record's reference count. When all pointers to the record have been collected (no references), the record will be evicted.

The eviction delay can be controlled with minTTL. It specifies a grace period during which the record will not be evicted even if it has no references. maxTTL specifies the maximum age of a record.
