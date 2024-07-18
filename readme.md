# phost
A process host, a daemon.

## usage

Run the daemon with a jobspec file. (To get staretd you can `cp phost-jobspec.example phost-jobspec`)

```
./phost
# or optional argument
./phost phost-jobspec
```

- a jobspec file is newline delimited file of commands to be executed
- a line is a job descrition, command and arguments are space separated
- jobs will be monitored and restarted if they exit
- PIDs are listed in the `phost` stdout, you can send kill signals to those processes and watch them restart

## design

Starting/stopping/monitoring different processes is inherently event driven. I was hacking on an event_loop and thought it would work well for this. The `RegisterListener/Dispatch` interface which sits between the event_loop and the daemon ended up working great.

The daemon needs work, but the general structure of the eventloop is good. There is an event queue and a work stack. The process exits when they are both empty, and relies on the golang scheduler to juggle concurrent work.

## disclaimer

This is an example repository. It is not stable, there are no versions or releases, and it is not production ready.
