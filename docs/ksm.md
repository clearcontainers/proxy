# Kernel Same-Page tuning

This section describes why `cc-proxy` tunes the host kernel
Kernel Same-Page ([KSM](https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/tree/Documentation/vm/ksm.txt))
settings and how it does it.

## What is KSM?

KSM is a host Linux* kernel feature for de-duplicating memory pages.
Although it was initially designed as a KVM specific feature, it is
now part of the generic Linux memory management subsystem and can
be leveraged by any userspace component or application looking for
memory to save.

A daemon (`ksmd`) periodically scans userspace memory, looking for
identical pages that can be replaced by a single, write-protected
page. When a process tries to modify this shared page content, it
gets a private copy into its memory space. KSM only scans and merges
pages that are both anonymous and that have been explictly tagged as
mergeable by applications calling into the `madvise` system call
(`int madvice(addr, length, MADV_MERGEABLE)`).

KSM is customizable through a set of Linux kernel `sysfs` attributes,
the most interesting ones being:

  * `/sys/kernel/mm/ksm/run`: Turns KSM on (`1`) and off (`0`).
  * `/sys/kernel/mm/ksm/sleep_millisec`: Knob that specifies the KSM
    scanning period.
  * `/sys/kernel/mm/ksm/pages_to_scan`: Sets the number of
    pages KSM will scan per scanning cycle.

The memory density improvements that KSM can provide come at a cost.
Depending on the number of anonymous pages it will scan, it can be
relatively expensive on CPU utilization.

## `cc-proxy` and KSM

The ideal KSM use case is when a given system runs a large amount
of identical and relatively long running virtual machines (VMs).
This is because when all the VMs run the same code, share the same
data and start the same processes, they will be allocating a lot of
identical pages that KSM can process and de-duplicate into shared
single pages.

A system running Intel® Clear Containers fits the above use case
precisely. Using KSM appropriately in Clear Container sytems can
significantly reduce memory footprint. This is why `cc-proxy` tries
to optimally tune KSM.

`cc-proxy` is the right component to tune KSM for the following reasons:

  * KSM settings are system wide ones that should be controlled by
    a system daemon like `cc-proxy`.
  * `cc-proxy` is where you can optimize KSM settings based on
     container creation activities.

## Theory of operation

`cc-proxy` supports three KSM modes, available through a command line
option: `cc-proxy -ksm`.

  1. `initial`: In this mode, `cc-proxy` does not take control of the
     system KSM settings. It does not turn KSM on or off, and does not
     modify any of the KSM settings.

  2. `off`: When running with `-ksm off`, `cc-proxy` will completely
     disable KSM on the system.

  3. `throttle`: In `throttle` mode, `cc-proxy` will throttle KSM up
     and down depending on the Clear Containers creation activity. This
     is the `cc-proxy` default KSM mode.

### KSM throttling

By default, `cc-proxy` will throttle KSM up and down. Regardless of
the current KSM system settings, `cc-proxy` will move them to the
`aggressive` settings as soon as a new Clear Container is created.
With the `aggressive` setting, ksmd will run every millisecond and
will scan 10% of all available anonymous pages during each scanning
cycle.

After switching to the `aggressive` KSM settings, `cc-proxy` will
throttle down to the `standard` setting if there are no additional
Clear Containers instances created for 30 seconds.
Then `cc-proxy` will continue throttling down to the `slow` KSM
setting if there no further Clear Container creation for the next
two minutes.
Finally, `cc-proxy` will get back to the initial KSM settings after
two more minutes, unless a new Clear Container is created.

At any point in time, `cc-proxy` will get back to to the `aggressive`
setting when detecting the creation of a Clear Container:

```
        +----------------+
        |                |
        |    Initial     |
        |    Settings    |<<-------------------------------+
        |                |                                 |
        +-------+--------+                                 |
                |                                          |
        New     |                                          |
     Container  |                                          |
                |                                          |
                v                                          |
         +--------------+                                  |
         |  Aggressive  |<<--------+                       |
         +--------------+          |                       |
                |                  |                       |
  No Container  |                  |                       |
     (30s)      |                  |    New                |
                |                  |  Container            |    No Container
                v                  |                       |       (2mn)
         +--------------+          |                       |
         |   Standard   |----------+                       |
         +--------------+          |                       |
                |                  |                       |
  No Container  |                  |                       |
     (2mn)      |                  |    New                |
                |                  |  Container            |
                v                  |                       |
         +--------------+          |                       |
         |     Slow     |----------+                       |
         +--------------+                                  |
                |                                          |
                |                                          |
                +------------------------------------------+

```