# 6.824的实验代码

首页：https://pdos.csail.mit.edu/6.824/index.html


## lab1: mapreduce实现

详细实验过程，可以`cd lab1_mapreduce`

### 实验经验

1. 最开始设计上没有仔细根据mapreduce的特性设计，导致走了些弯路：简单的采用了生产者/消费者队列(worker-pull)的方式获取任务，导致在crash-recover和reduce这里走了些弯路（应该直接一开始根据文件和参数生成好task-map，push给worker，后面会轻松很多，包括任务等待也会简单些）。
2. 对分布式系统中的crash-recover机制处理不太熟，一般代码编写只用考虑异常处理，分布式系统还要考虑某个进程的异常退出，一般用心跳机制。
3. 对sync.map进行update操作时，要深拷贝一块内存出来修改该值，再load回去，因为sync.map只保证他提供的方法线程安全，而直接对value中的内存赋值则无法保证。
