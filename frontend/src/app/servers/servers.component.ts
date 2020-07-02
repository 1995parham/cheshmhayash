import { Component, OnInit } from '@angular/core';
import { NATSService } from '../nats/nats.service';

@Component({
  selector: 'app-servers',
  templateUrl: './servers.component.html',
  styleUrls: ['./servers.component.scss']
})
export class ServersComponent implements OnInit {

  servers: string[];

  constructor(private natsService: NATSService) {  }

  ngOnInit(): void {
    this.natsService.list().subscribe(servers => this.servers = servers);
  }

}
