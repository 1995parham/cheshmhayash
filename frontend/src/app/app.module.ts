import { BrowserModule } from '@angular/platform-browser';
import { NgModule } from '@angular/core';
import { HttpClientModule } from '@angular/common/http';

import { AppRoutingModule } from './app-routing.module';
import { AppComponent } from './app.component';
import { NATSService } from './nats/nats.service';

import { NoopAnimationsModule } from '@angular/platform-browser/animations';
import { ServerComponent } from './server/server.component';
import { ServersComponent } from './servers/servers.component';
import { DashboardComponent } from './dashboard/dashboard.component';
import { ConnectionsComponent } from './connections/connections.component';

import { NzLayoutModule } from 'ng-zorro-antd/layout';
import { NzMenuModule } from 'ng-zorro-antd/menu';
import { NzDescriptionsModule } from 'ng-zorro-antd/descriptions';
import { NzGridModule } from 'ng-zorro-antd/grid';
import { NzSpaceModule } from 'ng-zorro-antd/space';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzPageHeaderModule } from 'ng-zorro-antd/page-header';
import { NzStatisticModule } from 'ng-zorro-antd/statistic';
import { NzTableModule } from 'ng-zorro-antd/table';

@NgModule({
  declarations: [
    AppComponent,
    ServerComponent,
    ServersComponent,
    DashboardComponent,
    ConnectionsComponent
  ],
  imports: [
    BrowserModule,
    AppRoutingModule,
    HttpClientModule,
    NoopAnimationsModule,
    NzLayoutModule,
    NzDescriptionsModule,
    NzGridModule,
    NzSpaceModule,
    NzCardModule,
    NzPageHeaderModule,
    NzMenuModule,
    NzTableModule,
    NzStatisticModule
  ],
  providers: [
    NATSService,
  ],
  bootstrap: [AppComponent]
})
export class AppModule { }
