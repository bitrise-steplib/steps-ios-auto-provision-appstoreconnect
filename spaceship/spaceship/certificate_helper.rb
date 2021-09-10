# require_relative 'certificate_info'
require_relative 'utils'
require_relative 'portal/certificate_client'

# CertificateHelper ...
class CertificateHelper
  def list_dev_certs
    certs = Portal::CertificateClient.download_development_certificates
    puts certs
    get_cert_infos(certs)
  end

  def list_dist_certs
    get_cert_infos(Portal::CertificateClient.download_production_certificates)
  end

  def get_cert_infos(portal_certificates)
    Log.debug('Certificates on Apple Developer Portal:')
    cert_infos = []
    portal_certificates.each do |cert|
      downloaded_portal_cert = cert.download
      base64_pem = Base64.encode64(downloaded_portal_cert.to_pem)

      cert_info = {
        content: base64_pem,
        id: cert.id
      }

      cert_infos.append(cert_info)

      Log.debug("- #{cert.name}: #{certificate_name_and_serial(downloaded_portal_cert)} expire: #{downloaded_portal_cert.not_after}")
    end

    cert_infos
  end
end